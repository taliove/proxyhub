package detection

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestExamRegions_TableComplete(t *testing.T) {
	if len(examRegions) != 8 {
		t.Fatalf("examRegions = %d, want 8 fixed regions", len(examRegions))
	}
	seenCode := map[string]bool{}
	seenURL := map[string]bool{}
	for i, r := range examRegions {
		if r.Code == "" || r.Name == "" || r.URL == "" {
			t.Errorf("region[%d] = %+v, want all fields non-empty", i, r)
		}
		if !strings.HasPrefix(r.URL, "https://") {
			t.Errorf("region[%d] URL = %q, want https", i, r.URL)
		}
		if seenCode[r.Code] {
			t.Errorf("duplicate region code %q", r.Code)
		}
		if seenURL[r.URL] {
			t.Errorf("duplicate region URL %q", r.URL)
		}
		seenCode[r.Code] = true
		seenURL[r.URL] = true
	}
}

func TestRunRegionSpeedSampler_SerialAndOrder(t *testing.T) {
	regions := []Region{{Code: "a", Name: "A"}, {Code: "b", Name: "B"}, {Code: "c", Name: "C"}}

	var active atomic.Int32
	var overlap atomic.Bool
	var probed []string
	probe := func(_ context.Context, r Region) RegionResult {
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		probed = append(probed, r.Code)
		active.Add(-1)
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 30, DownMbps: 12.5}
	}

	var emitted []string
	results := runRegionSpeedSampler(context.Background(), regions, probe, func(r RegionResult) {
		emitted = append(emitted, r.Code)
	})

	if overlap.Load() {
		t.Error("region probes overlapped: serial exclusivity violated")
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if i >= len(probed) || probed[i] != w {
			t.Fatalf("probe order = %v, want %v", probed, want)
		}
		if i >= len(emitted) || emitted[i] != w {
			t.Fatalf("emit order = %v, want %v", emitted, want)
		}
		if results[i].Code != w {
			t.Fatalf("results = %v, want ordered %v", results, want)
		}
	}
}

func TestRunRegionSpeedSampler_SingleFailureContinues(t *testing.T) {
	regions := []Region{{Code: "a", Name: "A"}, {Code: "b", Name: "B"}, {Code: "c", Name: "C"}}
	probe := func(_ context.Context, r Region) RegionResult {
		if r.Code == "b" {
			return RegionResult{Code: r.Code, Name: r.Name, Error: "boom"}
		}
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 20, DownMbps: 9}
	}

	results := runRegionSpeedSampler(context.Background(), regions, probe, nil)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 (failure does not drop the row)", len(results))
	}
	if results[1].Error != "boom" || results[1].DownMbps != 0 {
		t.Errorf("failed row = %+v, want error set, no speed", results[1])
	}
	if results[0].Error != "" || results[2].Error != "" {
		t.Errorf("other rows should succeed: %+v %+v", results[0], results[2])
	}
}

func TestRunRegionSpeedSampler_StopsOnCancel(t *testing.T) {
	regions := []Region{{Code: "a"}, {Code: "b"}, {Code: "c"}}
	ctx, cancel := context.WithCancel(context.Background())
	var probed []string
	probe := func(_ context.Context, r Region) RegionResult {
		probed = append(probed, r.Code)
		cancel() // cancel after the first region
		return RegionResult{Code: r.Code}
	}
	runRegionSpeedSampler(ctx, regions, probe, nil)
	if len(probed) != 1 || probed[0] != "a" {
		t.Errorf("probed = %v, want [a] (cancel stops before b)", probed)
	}
}

func TestRegionSpeedStage_EmitsRowsThenMetrics(t *testing.T) {
	regions := []Region{{Code: "a", Name: "A"}, {Code: "b", Name: "B"}}
	probe := func(_ context.Context, r Region) RegionResult {
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 15, DownMbps: 42}
	}
	stage := regionSpeedStage(regions, probe)
	if stage.name != "region_speed" {
		t.Fatalf("stage name = %q, want region_speed", stage.name)
	}

	var rowEvents, sectionDone int
	var lastRowAt, sectionDoneAt = -1, -1
	var report ExamReport
	i := 0
	stage.run(context.Background(), func(e ExamEvent) {
		switch e.Phase {
		case "region":
			rowEvents++
			lastRowAt = i
			if e.Section != "region_speed" || e.Region == nil {
				t.Errorf("bad region event: %+v", e)
			}
		case "section_done":
			sectionDone++
			sectionDoneAt = i
			if e.RegionSpeed == nil || len(e.RegionSpeed.Regions) != 2 {
				t.Errorf("section_done metrics = %+v, want 2 regions", e.RegionSpeed)
			}
		}
		i++
	}, &report)

	if rowEvents != 2 {
		t.Errorf("region row events = %d, want 2", rowEvents)
	}
	if sectionDone != 1 {
		t.Errorf("section_done events = %d, want 1", sectionDone)
	}
	if !(lastRowAt < sectionDoneAt) {
		t.Errorf("rows must precede section_done (lastRow=%d sectionDone=%d)", lastRowAt, sectionDoneAt)
	}
	if report.RegionSpeed == nil || len(report.RegionSpeed.Regions) != 2 {
		t.Errorf("report.RegionSpeed = %+v, want 2 regions", report.RegionSpeed)
	}
}

// TestExamStream_StageOrder 校验新段序:出网 -> 稳定性 -> 多地域 -> 解锁,全部串行。
// 出网段前置且跑完才进稳定性采样(采样独占不破坏)。四类探测器均注入假实现,保持快速确定、不触网。
func TestExamStream_StageOrder(t *testing.T) {
	node := &subscription.Node{Name: "n", Server: "example.com", Port: 443, Type: "vmess"}
	d := NewDetector(1, time.Second, time.Second)
	d.SetExamConfigProvider(func() ExamConfig {
		return ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 500, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 1}
	})
	d.SetStabilityProbeFactory(func(*subscription.Node) (StabilityProbe, error) {
		return func(context.Context) (int, bool) { return 30, true }, nil
	})
	d.SetRegionSpeedProbeFactory(func(*subscription.Node) (RegionSpeedProbe, error) {
		return func(_ context.Context, r Region) RegionResult {
			return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 40, DownMbps: 20}
		}, nil
	})
	d.SetUnlockProbeFactory(func(*subscription.Node) (UnlockProbe, error) {
		return func(_ context.Context, target Target) Result {
			return Result{TargetName: target.Name, Available: true, Level: LevelFull, Region: "US"}
		}, nil
	})
	d.SetEgressProbeFactory(func(*subscription.Node) (EgressProbe, error) {
		return EgressProbe{
			IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7", Country: "United States"} },
			IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
			DNS: func(context.Context) EgressDNS {
				return EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example"}
			},
		}, nil
	})

	var phases []string
	egressDoneAt, firstSampleAt, stabilityDoneAt := -1, -1, -1
	firstRegionAt, regionDoneAt := -1, -1
	firstUnlockAt, unlockDoneAt, doneAt := -1, -1, -1
	var firstRegionName string
	report := d.ExamStream(context.Background(), node, func(e ExamEvent) {
		i := len(phases)
		phases = append(phases, e.Phase+"/"+e.Section)
		switch {
		case e.Phase == "section_done" && e.Section == "egress":
			egressDoneAt = i
		case e.Phase == "sample":
			if firstSampleAt == -1 {
				firstSampleAt = i
			}
		case e.Phase == "section_done" && e.Section == "stability":
			stabilityDoneAt = i
		case e.Phase == "region":
			if firstRegionAt == -1 {
				firstRegionAt = i
				firstRegionName = e.Region.Name
			}
		case e.Phase == "section_done" && e.Section == "region_speed":
			regionDoneAt = i
		case e.Phase == "unlock":
			if firstUnlockAt == -1 {
				firstUnlockAt = i
			}
		case e.Phase == "section_done" && e.Section == "unlock":
			unlockDoneAt = i
		case e.Phase == "done":
			doneAt = i
		}
	})

	if egressDoneAt == -1 || firstSampleAt == -1 || stabilityDoneAt == -1 || firstRegionAt == -1 ||
		regionDoneAt == -1 || firstUnlockAt == -1 || unlockDoneAt == -1 || doneAt == -1 {
		t.Fatalf("missing key events: %v", phases)
	}
	// 出网段独占:其 section_done 在稳定性首个采样之前(跑完才进采样)。
	if egressDoneAt >= firstSampleAt {
		t.Errorf("egress must finish before stability sampling: egressDone=%d firstSample=%d (%v)",
			egressDoneAt, firstSampleAt, phases)
	}
	// 全序:出网 -> 稳定性 -> 多地域 -> 解锁 -> done。
	if !(firstSampleAt < stabilityDoneAt && stabilityDoneAt < firstRegionAt &&
		firstRegionAt < regionDoneAt && regionDoneAt < firstUnlockAt &&
		firstUnlockAt < unlockDoneAt && unlockDoneAt < doneAt) {
		t.Errorf("stage order wrong: egressDone=%d firstSample=%d stabilityDone=%d firstRegion=%d regionDone=%d firstUnlock=%d unlockDone=%d done=%d (%v)",
			egressDoneAt, firstSampleAt, stabilityDoneAt, firstRegionAt, regionDoneAt, firstUnlockAt, unlockDoneAt, doneAt, phases)
	}
	if firstRegionName != "基准" {
		t.Errorf("first region row = %q, want 基准 (baseline is the first multi-region row)", firstRegionName)
	}
	if report.Egress == nil || report.Stability == nil || report.RegionSpeed == nil || report.Unlock == nil {
		t.Errorf("report missing sections: %+v", report)
	}
	// 基准行 + 8 区 = len(examRegions)+1。
	if len(report.RegionSpeed.Regions) != len(examRegions)+1 {
		t.Errorf("region rows = %d, want %d (baseline + %d regions)", len(report.RegionSpeed.Regions), len(examRegions)+1, len(examRegions))
	}
}

func TestDrainForDuration_ComputesMbpsAndBounds(t *testing.T) {
	// 无限数据源:切片时长到点即停,不得卡死;deadlineReader 返回干净 EOF(err==nil)。
	inf := &infiniteReader{}
	start := time.Now()
	mbps, n, err := drainForDuration(inf, 40*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("drainForDuration did not stop promptly: %v", elapsed)
	}
	if err != nil {
		t.Errorf("clean deadline stop should not error, got %v", err)
	}
	if mbps <= 0 || n <= 0 {
		t.Errorf("mbps=%v n=%d, want both > 0", mbps, n)
	}
}

func TestRegionDownloadResult_FailureVsPartial(t *testing.T) {
	r := Region{Code: "x", Name: "X"}
	brokenLow := regionDownloadResult(r, 10*time.Millisecond, 0, minValidDownloadBytes-1, errBoom)
	if brokenLow.Error == "" {
		t.Error("mid-stream failure with insufficient bytes should be an error row")
	}
	// 硬超时切断但已读够数据 -> 用已读速率视为成功(不丢样本)。
	partialOK := regionDownloadResult(r, 10*time.Millisecond, 7.5, minValidDownloadBytes, errBoom)
	if partialOK.Error != "" || partialOK.DownMbps != 7.5 {
		t.Errorf("partial-but-sufficient should succeed with speed, got %+v", partialOK)
	}
	clean := regionDownloadResult(r, 10*time.Millisecond, 12, minValidDownloadBytes*4, nil)
	if clean.Error != "" || clean.DownMbps != 12 {
		t.Errorf("clean read should succeed, got %+v", clean)
	}
}

func TestRegionSpeedErrorStage_EmitsErrorRows(t *testing.T) {
	regions := []Region{{Code: "a", Name: "A"}, {Code: "b", Name: "B"}}
	stage := regionSpeedErrorStage(regions, errBoom)
	var rows, sectionDone int
	var report ExamReport
	stage.run(context.Background(), func(e ExamEvent) {
		switch e.Phase {
		case "region":
			rows++
			if e.Region == nil || e.Region.Error == "" {
				t.Errorf("degraded region row should carry error: %+v", e.Region)
			}
		case "section_done":
			sectionDone++
		}
	}, &report)
	if rows != 2 || sectionDone != 1 {
		t.Errorf("rows=%d sectionDone=%d, want 2 and 1", rows, sectionDone)
	}
	if report.RegionSpeed == nil || len(report.RegionSpeed.Regions) != 2 {
		t.Errorf("degraded stage should still record metrics rows: %+v", report.RegionSpeed)
	}
}

var errBoom = fmt.Errorf("boom")

func TestMeasureRegionSpeed_SuccessAndError(t *testing.T) {
	body := strings.Repeat("x", 600*1024) // > minValidDownloadBytes
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer bad.Close()

	client := &http.Client{}
	slice := 50 * time.Millisecond
	hard := 3 * time.Second

	good := measureRegionSpeed(context.Background(), client, Region{Code: "ok", Name: "OK", URL: ok.URL}, slice, hard)
	if good.Error != "" {
		t.Fatalf("good region errored: %q", good.Error)
	}
	if good.TTFBms < 0 || good.DownMbps <= 0 {
		t.Errorf("good result = %+v, want ttfb>=0 and down>0", good)
	}

	failed := measureRegionSpeed(context.Background(), client, Region{Code: "bad", Name: "BAD", URL: bad.URL}, slice, hard)
	if failed.Error == "" {
		t.Errorf("bad region (403) should carry an error, got %+v", failed)
	}
}

// --- 基准对照行:多地域段第一行,name="基准",Cloudflare 就近 POP ---

func TestExamRegionsWithBaseline_BaselineFirst(t *testing.T) {
	regions := examRegionsWithBaseline()
	if len(regions) != len(examRegions)+1 {
		t.Fatalf("len = %d, want %d (baseline + %d regions)", len(regions), len(examRegions)+1, len(examRegions))
	}
	base := regions[0]
	if base.Name != "基准" {
		t.Errorf("first row name = %q, want 基准", base.Name)
	}
	if base.Code == "" {
		t.Error("baseline code must be non-empty (stable key)")
	}
	if !strings.Contains(base.URL, "speed.cloudflare.com") {
		t.Errorf("baseline URL = %q, want Cloudflare down endpoint", base.URL)
	}
	// 其后必须是原样 8 区(基准不污染 examRegions)。
	for i, r := range examRegions {
		if regions[i+1].Code != r.Code {
			t.Errorf("region[%d] = %q, want %q (baseline must not reorder regions)", i+1, regions[i+1].Code, r.Code)
		}
	}
	if len(examRegions) != 8 {
		t.Errorf("examRegions mutated: len = %d, want 8", len(examRegions))
	}
}

func TestRegionSpeedStage_BaselineRowEmittedFirst(t *testing.T) {
	probe := func(_ context.Context, r Region) RegionResult {
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 20, DownMbps: 30}
	}
	stage := regionSpeedStage(examRegionsWithBaseline(), probe)

	var firstRowName string
	seen := false
	var report ExamReport
	stage.run(context.Background(), func(e ExamEvent) {
		if e.Phase == "region" && !seen {
			seen = true
			firstRowName = e.Region.Name
		}
	}, &report)

	if firstRowName != "基准" {
		t.Errorf("first emitted region row = %q, want 基准", firstRowName)
	}
	if report.RegionSpeed == nil || len(report.RegionSpeed.Regions) != len(examRegions)+1 {
		t.Errorf("report rows = %+v, want %d", report.RegionSpeed, len(examRegions)+1)
	}
	if report.RegionSpeed.Regions[0].Name != "基准" {
		t.Errorf("report first row = %q, want 基准", report.RegionSpeed.Regions[0].Name)
	}
}

// --- 单区(含基准)失败自动重试:捞回 / 耗尽 / 成功不重试 / 串行独占不回归 ---

func TestWithRegionRetry_RecoversAfterRetry(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, r Region) RegionResult {
		calls++
		if calls == 1 {
			return RegionResult{Code: r.Code, Name: r.Name, Error: "dial tcp: i/o timeout"}
		}
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 25, DownMbps: 40}
	}
	res := withRegionRetry(probe)(context.Background(), Region{Code: "a", Name: "A"})
	if res.Error != "" {
		t.Errorf("result error = %q, want recovered (empty)", res.Error)
	}
	if res.DownMbps != 40 {
		t.Errorf("down = %v, want 40 (second attempt result)", res.DownMbps)
	}
	if calls != 2 {
		t.Errorf("probe calls = %d, want 2 (one retry)", calls)
	}
}

func TestWithRegionRetry_ExhaustsToError(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, r Region) RegionResult {
		calls++
		return RegionResult{Code: r.Code, Name: r.Name, Error: "boom"}
	}
	res := withRegionRetry(probe)(context.Background(), Region{Code: "a", Name: "A"})
	if res.Error == "" {
		t.Error("exhausted retries should still carry error")
	}
	if calls != examRegionMaxRetries+1 {
		t.Errorf("probe calls = %d, want %d (initial + retries)", calls, examRegionMaxRetries+1)
	}
}

func TestWithRegionRetry_SuccessNoRetry(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, r Region) RegionResult {
		calls++
		return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 10, DownMbps: 99}
	}
	res := withRegionRetry(probe)(context.Background(), Region{Code: "a", Name: "A"})
	if res.DownMbps != 99 || calls != 1 {
		t.Errorf("success path: down=%v calls=%d, want 99 and 1 call", res.DownMbps, calls)
	}
}

// TestWithRegionRetry_SerialInSampler 重试不破坏串行独占:即便每区首探失败触发重试,
// 任一时刻只有一路探测在飞,且每区只 emit 最终一行(重试期间不 emit 中间失败态)。
func TestWithRegionRetry_SerialInSampler(t *testing.T) {
	regions := []Region{{Code: "a", Name: "A"}, {Code: "b", Name: "B"}}
	var active atomic.Int32
	var overlap atomic.Bool
	attempt := map[string]int{}
	probe := func(_ context.Context, r Region) RegionResult {
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		attempt[r.Code]++
		res := RegionResult{Code: r.Code, Name: r.Name, TTFBms: 15, DownMbps: 20}
		if attempt[r.Code] == 1 {
			res = RegionResult{Code: r.Code, Name: r.Name, Error: "connection reset by peer"}
		}
		active.Add(-1)
		return res
	}

	var emitted []RegionResult
	results := runRegionSpeedSampler(context.Background(), regions, withRegionRetry(probe), func(r RegionResult) {
		emitted = append(emitted, r)
	})

	if overlap.Load() {
		t.Error("retry overlapped probes: serial exclusivity violated")
	}
	if len(emitted) != 2 {
		t.Fatalf("emitted rows = %d, want 2 (one final row per region, no intermediate failure)", len(emitted))
	}
	for i, r := range emitted {
		if r.Error != "" {
			t.Errorf("emitted[%d] = %+v, want recovered final row (no error)", i, r)
		}
	}
	for _, r := range results {
		if r.Error != "" {
			t.Errorf("result %+v should be recovered after retry", r)
		}
	}
}

// infiniteReader 永远返回满缓冲数据,用于验证 drainForDuration 的时长边界。
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
