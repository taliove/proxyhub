package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

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
	srv, _ := newTestServer(t, []*subscription.Node{node})

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

	// 序列断言:稳定性 sample(N) -> 稳定性 section_done(metrics)
	//         -> 多地域 region 行(逐区) -> 多地域 section_done(region_speed)
	//         -> 解锁 unlock 行(逐个) -> 解锁 section_done(unlock) -> done。
	var sampleCount, regionRowCount, unlockRowCount = 0, 0, 0
	var stabilityDoneAt, firstRegionAt, regionDoneAt = -1, -1, -1
	var firstUnlockAt, unlockDoneAt, doneAt = -1, -1, -1
	for i, f := range frames {
		switch f["phase"] {
		case "sample":
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
	if stabilityDoneAt == -1 || firstRegionAt == -1 || regionDoneAt == -1 ||
		firstUnlockAt == -1 || unlockDoneAt == -1 || doneAt == -1 {
		t.Fatalf("missing key frames: stabilityDone=%d firstRegion=%d regionDone=%d firstUnlock=%d unlockDone=%d done=%d",
			stabilityDoneAt, firstRegionAt, regionDoneAt, firstUnlockAt, unlockDoneAt, doneAt)
	}
	// 三段严格有序,且解锁 section_done 在所有 unlock 行之后。
	if !(stabilityDoneAt < firstRegionAt && firstRegionAt < regionDoneAt &&
		regionDoneAt < firstUnlockAt && firstUnlockAt < unlockDoneAt && unlockDoneAt < doneAt) {
		t.Errorf("order wrong: stabilityDone=%d firstRegion=%d regionDone=%d firstUnlock=%d unlockDone=%d done=%d",
			stabilityDoneAt, firstRegionAt, regionDoneAt, firstUnlockAt, unlockDoneAt, doneAt)
	}
	if doneAt != len(frames)-1 {
		t.Errorf("done at %d, want last frame (%d)", doneAt, len(frames)-1)
	}
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
