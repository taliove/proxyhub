package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// waitExamJobSettled 等待体检任务完全收口(jobs 行离开 running + 历史已落库)。
// OnComplete(落历史 + 标签重算 + 区域回写)由 jobs 管理器在 finalize 后经独立
// goroutine 触发,与 SSE drain 结束无先后保证;jobs 行 Finish 后于 OnComplete,
// 故本条件覆盖全部异步 DB 写入。不等待就返回会让 t.TempDir 清理与异步 WAL
// 写入竞态(CI Linux 上 directory not empty,与 5ac2bd0 同类)。
func waitExamJobSettled(t *testing.T, st *store.Store, nodeKey string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		running := false
		if recs, err := st.Jobs().LoadRunning(); err == nil {
			for _, r := range recs {
				if r.Kind == "exam" && r.Key == nodeKey {
					running = true
					break
				}
			}
		}
		entry, herr := st.LatestCompleteExamHistory(nodeKey)
		if !running && herr == nil && entry != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("exam job not settled within 5s (running=%v, history saved=%v)", running, entry != nil)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// parseSSEFrames 从 SSE 响应体拆出每个 "data: {...}" 帧并解析为 map。
func parseSSEFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("parse frame %q: %v", raw, err)
		}
		frames = append(frames, m)
	}
	return frames
}

func TestHandleNodeExamStream_EventSequence(t *testing.T) {
	node := &subscription.Node{Name: "exam-node", Server: "example.com", Port: 443, Type: "vmess", Source: "airport"}
	srv, st := newTestServer(t, []*subscription.Node{node})

	// 注入假探测器(不触真实网络)+ 短配置(3 次采样,间隔小),让 SSE 迅速跑完。
	det := srv.detectionService.detector
	det.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		lat := []int{40, 55, 48}
		i := 0
		return func(context.Context) (int, bool) {
			v := lat[i%len(lat)]
			i++
			return v, true
		}, nil
	})
	det.SetExamConfigProvider(func() detection.ExamConfig {
		return detection.ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 300, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 5}
	})
	// 多地域段也注入假测速器(不触真实网络),逐区返回固定结果。
	det.SetRegionSpeedProbeFactory(func(*subscription.Node) (detection.RegionSpeedProbe, error) {
		return func(_ context.Context, r detection.Region) detection.RegionResult {
			return detection.RegionResult{Code: r.Code, Name: r.Name, TTFBms: 42, DownMbps: 18.5}
		}, nil
	})
	// 解锁段注入假探测器(不触真实网络),逐目标返回固定判定。
	det.SetUnlockProbeFactory(func(*subscription.Node) (detection.UnlockProbe, error) {
		return func(_ context.Context, target detection.Target) detection.Result {
			return detection.Result{TargetName: target.Name, Available: true, Level: detection.LevelFull, Region: "US"}
		}, nil
	})
	// 出网信息段注入假探测器(不触真实网络):IPv4/IPv6/DNS 各返回固定结果。
	det.SetEgressProbeFactory(func(*subscription.Node) (detection.EgressProbe, error) {
		return detection.EgressProbe{
			IPv4: func(context.Context) detection.EgressIPv4 {
				return detection.EgressIPv4{IP: "203.0.113.7", Country: "United States", CountryCode: "US", Hosting: true}
			},
			IPv6: func(context.Context) detection.EgressIPv6 { return detection.EgressIPv6{Available: false} },
			DNS: func(context.Context) detection.EgressDNS {
				return detection.EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example DNS"}
			},
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	frames := parseSSEFrames(t, w.Body.String())
	if len(frames) < 3 {
		t.Fatalf("frames = %d, want >=3 (samples + section_done + done)", len(frames))
	}

	// 序列断言(新段序,全部串行):
	//   出网 egress 行(ipv4/ipv6/dns) -> 出网 section_done(egress)
	//     -> 稳定性 sample(N) -> 稳定性 section_done(metrics)
	//     -> 多地域 region 行(逐区) -> 多地域 section_done(region_speed)
	//     -> 解锁 unlock 行 -> 解锁 section_done(unlock) -> done。
	// 出网段前置且独占:其 section_done 在首个稳定性采样之前(跑完才进采样)。
	var sampleCount, regionRowCount, unlockRowCount, egressRowCount = 0, 0, 0, 0
	var firstEgressAt, egressDoneAt = -1, -1
	var firstSampleAt, stabilityDoneAt = -1, -1
	var firstRegionAt, regionDoneAt = -1, -1
	var firstUnlockAt, unlockDoneAt, doneAt = -1, -1, -1
	for i, f := range frames {
		switch f["phase"] {
		case "egress":
			if firstEgressAt == -1 {
				firstEgressAt = i
			}
			if f["section"] != "egress" || f["egress"] == nil {
				t.Errorf("egress[%d] frame malformed: %v", i, f)
			}
			egressRowCount++
		case "sample":
			if firstSampleAt == -1 {
				firstSampleAt = i
			}
			if stabilityDoneAt != -1 {
				t.Errorf("stability sample appeared after its section_done (index %d)", i)
			}
			if f["section"] != "stability" {
				t.Errorf("sample[%d] section = %v, want stability", i, f["section"])
			}
			sampleCount++
		case "region":
			if firstRegionAt == -1 {
				firstRegionAt = i
			}
			if f["section"] != "region_speed" || f["region"] == nil {
				t.Errorf("region[%d] frame malformed: %v", i, f)
			}
			regionRowCount++
		case "unlock":
			if firstUnlockAt == -1 {
				firstUnlockAt = i
			}
			if f["section"] != "unlock" || f["unlock_result"] == nil {
				t.Errorf("unlock[%d] frame malformed: %v", i, f)
			}
			unlockRowCount++
		case "section_done":
			switch f["section"] {
			case "egress":
				egressDoneAt = i
				if _, ok := f["egress"]; !ok {
					t.Errorf("egress section_done missing egress metrics")
				}
			case "stability":
				stabilityDoneAt = i
				if _, ok := f["metrics"]; !ok {
					t.Errorf("stability section_done missing metrics")
				}
			case "region_speed":
				regionDoneAt = i
				if _, ok := f["region_speed"]; !ok {
					t.Errorf("region_speed section_done missing region_speed metrics")
				}
			case "unlock":
				unlockDoneAt = i
				if _, ok := f["unlock"]; !ok {
					t.Errorf("unlock section_done missing unlock metrics")
				}
			default:
				t.Errorf("unexpected section_done section %v", f["section"])
			}
		case "done":
			doneAt = i
		}
	}

	if sampleCount != 3 {
		t.Errorf("sample frames = %d, want 3", sampleCount)
	}
	if regionRowCount == 0 {
		t.Error("no region row frames emitted")
	}
	if unlockRowCount != len(detection.DefaultUnlockTargets()) {
		t.Errorf("unlock row frames = %d, want %d", unlockRowCount, len(detection.DefaultUnlockTargets()))
	}
	if egressRowCount != 3 {
		t.Errorf("egress row frames = %d, want 3 (ipv4/ipv6/dns)", egressRowCount)
	}
	if firstEgressAt == -1 || egressDoneAt == -1 || firstSampleAt == -1 || stabilityDoneAt == -1 ||
		firstRegionAt == -1 || regionDoneAt == -1 || firstUnlockAt == -1 || unlockDoneAt == -1 || doneAt == -1 {
		t.Fatalf("missing key frames: firstEgress=%d egressDone=%d firstSample=%d stabilityDone=%d firstRegion=%d regionDone=%d firstUnlock=%d unlockDone=%d done=%d",
			firstEgressAt, egressDoneAt, firstSampleAt, stabilityDoneAt, firstRegionAt, regionDoneAt, firstUnlockAt, unlockDoneAt, doneAt)
	}
	// 四段严格串行:出网 -> 稳定性 -> 多地域 -> 解锁 -> done。
	if !(firstEgressAt < egressDoneAt && egressDoneAt < firstSampleAt &&
		firstSampleAt < stabilityDoneAt && stabilityDoneAt < firstRegionAt &&
		firstRegionAt < regionDoneAt && regionDoneAt < firstUnlockAt &&
		firstUnlockAt < unlockDoneAt && unlockDoneAt < doneAt) {
		t.Errorf("stage order wrong: firstEgress=%d egressDone=%d firstSample=%d stabilityDone=%d firstRegion=%d regionDone=%d firstUnlock=%d unlockDone=%d done=%d",
			firstEgressAt, egressDoneAt, firstSampleAt, stabilityDoneAt, firstRegionAt, regionDoneAt, firstUnlockAt, unlockDoneAt, doneAt)
	}
	if doneAt != len(frames)-1 {
		t.Errorf("done at %d, want last frame (%d)", doneAt, len(frames)-1)
	}

	// SSE 结束 ≠ 任务收口:OnComplete 的异步 DB 写入必须等完,
	// 否则测试返回时 t.TempDir 清理与之竞态(见 waitExamJobSettled 注释)。
	waitExamJobSettled(t, st, node.NodeKey())
}

func TestHandleNodeExamStream_NodeNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key=missing", nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
