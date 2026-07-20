package detection

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// unlockStubRoundTripper 按请求 Host 分发预设响应,隔离真实网络(用于验证注册表分发)。
type unlockStubRoundTripper struct {
	byHost map[string]struct {
		status int
		body   string
	}
}

func (s unlockStubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r, ok := s.byHost[req.URL.Host]
	if !ok {
		r.status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

// TestUnlockStage_RunsChecksInParallel 6 个解锁判定必须并行执行:
// 每个 probe 进入即阻塞,只有全部 6 个同时在飞才可能收满 started,否则串行会卡死(超时判失败)。
func TestUnlockStage_RunsChecksInParallel(t *testing.T) {
	targets := DefaultUnlockTargets()
	started := make(chan struct{}, len(targets))
	release := make(chan struct{})

	probe := func(_ context.Context, target Target) Result {
		started <- struct{}{}
		<-release
		return Result{TargetName: target.Name, Available: true, Level: LevelFull, Region: "US"}
	}

	stage := unlockStage(targets, probe, 2*time.Second)
	if stage.name != "unlock" {
		t.Fatalf("stage name = %q, want unlock", stage.name)
	}

	var report ExamReport
	var rows []Result
	var sectionDone int
	done := make(chan struct{})
	go func() {
		stage.run(context.Background(), func(e ExamEvent) {
			switch e.Phase {
			case "unlock":
				if e.Section != "unlock" || e.UnlockResult == nil {
					t.Errorf("bad unlock row event: %+v", e)
				}
				rows = append(rows, *e.UnlockResult)
			case "section_done":
				sectionDone++
				if e.Unlock == nil {
					t.Error("section_done missing unlock metrics")
				}
			}
		}, &report)
		close(done)
	}()

	// 全部 6 个 probe 必须同时在飞(证明并行)。串行则只会有 1 个 started,select 超时判失败。
	for i := 0; i < len(targets); i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d probes started concurrently: not parallel", i, len(targets))
		}
	}
	close(release)
	<-done

	if len(rows) != len(targets) {
		t.Errorf("unlock row events = %d, want %d", len(rows), len(targets))
	}
	if sectionDone != 1 {
		t.Errorf("section_done events = %d, want 1", sectionDone)
	}
	if report.Unlock == nil || len(report.Unlock.Results) != len(targets) {
		t.Fatalf("report.Unlock = %+v, want %d results", report.Unlock, len(targets))
	}
}

// TestUnlockStage_TotalTimeout 整段有总超时:probe 阻塞在 ctx 上,超时后整段迅速收尾且每个目标出错。
func TestUnlockStage_TotalTimeout(t *testing.T) {
	targets := DefaultUnlockTargets()
	probe := func(ctx context.Context, target Target) Result {
		<-ctx.Done()
		return Result{TargetName: target.Name, Available: false, Error: "ctx cancelled"}
	}

	stage := unlockStage(targets, probe, 50*time.Millisecond)

	var report ExamReport
	start := time.Now()
	stage.run(context.Background(), func(ExamEvent) {}, &report)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("segment took %v, total timeout not enforced", elapsed)
	}
	if report.Unlock == nil || len(report.Unlock.Results) != len(targets) {
		t.Fatalf("report.Unlock = %+v, want %d results", report.Unlock, len(targets))
	}
	for _, r := range report.Unlock.Results {
		if r.Error == "" {
			t.Errorf("target %q error empty, want non-empty on timeout", r.TargetName)
		}
	}
}

// TestUnlockStage_ResultsInTargetOrder report 内结果按目标顺序落位(并行完成顺序不影响 report)。
func TestUnlockStage_ResultsInTargetOrder(t *testing.T) {
	targets := DefaultUnlockTargets()
	probe := func(_ context.Context, target Target) Result {
		return Result{TargetName: target.Name, Available: true}
	}
	stage := unlockStage(targets, probe, time.Second)

	var report ExamReport
	stage.run(context.Background(), func(ExamEvent) {}, &report)
	if report.Unlock == nil {
		t.Fatal("report.Unlock nil")
	}
	for i, r := range report.Unlock.Results {
		if r.TargetName != targets[i].Name {
			t.Errorf("results[%d].TargetName = %q, want %q", i, r.TargetName, targets[i].Name)
		}
	}
}

// TestUnlockTargets_AllHaveRegisteredCheckers 体检段遍历 DefaultUnlockTargets 并经注册表分发,
// 六个 kind 必须都能在注册表命中,证明体检链路复用批量检测注册的判定器,无双份实现。
func TestUnlockTargets_AllHaveRegisteredCheckers(t *testing.T) {
	for _, target := range DefaultUnlockTargets() {
		kind, err := target.resolveKind()
		if err != nil {
			t.Errorf("target %q resolveKind: %v", target.Name, err)
			continue
		}
		if _, err := specializedChecker(kind); err != nil {
			t.Errorf("target %q kind %q not in registry: %v", target.Name, kind, err)
		}
	}
}

// TestNewUnlockProbe_DispatchesThroughRegistry newUnlockProbe 经注册表把 OpenAI 目标分发给已注册的
// checkOpenAI(而非另起实现):喂 200 + trace loc=US,期望 available/full/US。
func TestNewUnlockProbe_DispatchesThroughRegistry(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	client := &http.Client{Transport: unlockStubRoundTripper{byHost: map[string]struct {
		status int
		body   string
	}{
		"api.openai.com":     {status: http.StatusOK, body: `{"cookie_config":{}}`},
		"www.cloudflare.com": {status: http.StatusOK, body: "fl=1\nloc=US\nwarp=off\n"},
	}}}

	probe := newUnlockProbe(client, node)
	res := probe(context.Background(), Target{Name: "OpenAI", Kind: KindOpenAI})
	if !res.Available || res.Level != LevelFull {
		t.Fatalf("available=%v level=%q, want true/full (registry checker not invoked)", res.Available, res.Level)
	}
	if res.Region != "US" {
		t.Errorf("region = %q, want US", res.Region)
	}
}

// TestNewUnlockProbe_UnknownKind 未知 kind 走框架级错误结果,不 panic。
func TestNewUnlockProbe_UnknownKind(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	probe := newUnlockProbe(http.DefaultClient, node)
	res := probe(context.Background(), Target{Name: "Bad", Kind: Kind("nope")})
	if res.Available || res.Error == "" {
		t.Errorf("unknown kind mishandled: %+v", res)
	}
}

// --- 解锁重试:网络类失败重试;判定结论(full/blocked/originals_only)绝不重试 ---

// TestWithUnlockRetry_TransientRetried 传输类失败(Level 空 + 传输错误)先失败后成功:重试捞回,只返回最终判定。
func TestWithUnlockRetry_TransientRetried(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, target Target) Result {
		calls++
		if calls == 1 {
			return Result{TargetName: target.Name, Error: "request original title failed: read tcp: connection reset by peer"}
		}
		return Result{TargetName: target.Name, Available: true, Level: LevelFull, Region: "US"}
	}
	res := withUnlockRetry(probe)(context.Background(), Target{Name: "OpenAI", Kind: KindOpenAI})
	if res.Level != LevelFull || !res.Available {
		t.Errorf("result = %+v, want recovered full", res)
	}
	if calls != 2 {
		t.Errorf("probe calls = %d, want 2 (one retry)", calls)
	}
}

// TestWithUnlockRetry_VerdictNotRetried 三种判定结论都绝不重试(即便 Available=false 的 blocked)。
func TestWithUnlockRetry_VerdictNotRetried(t *testing.T) {
	verdicts := []Result{
		{Level: LevelFull, Available: true},
		{Level: LevelOriginalsOnly, Available: true},
		{Level: LevelBlocked, Available: false},
	}
	for _, v := range verdicts {
		v := v
		calls := 0
		probe := func(_ context.Context, target Target) Result {
			calls++
			return Result{TargetName: target.Name, Available: v.Available, Level: v.Level}
		}
		res := withUnlockRetry(probe)(context.Background(), Target{Name: "T", Kind: KindNetflix})
		if res.Level != v.Level {
			t.Errorf("level = %q, want %q", res.Level, v.Level)
		}
		if calls != 1 {
			t.Errorf("verdict %q retried (%d calls), want 1", v.Level, calls)
		}
	}
}

// TestWithUnlockRetry_InconclusiveNotRetried 非传输类失败(如状态码判定不确定)不重试。
func TestWithUnlockRetry_InconclusiveNotRetried(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, target Target) Result {
		calls++
		return Result{TargetName: target.Name, Error: "netflix classification inconclusive (status 500/500)"}
	}
	res := withUnlockRetry(probe)(context.Background(), Target{Name: "Netflix", Kind: KindNetflix})
	if res.Error == "" {
		t.Error("inconclusive result should keep its error")
	}
	if calls != 1 {
		t.Errorf("non-transient failure retried (%d calls), want 1", calls)
	}
}

// TestWithUnlockRetry_ExhaustsTransient 传输类失败始终不恢复:耗尽重试后仍返回失败。
func TestWithUnlockRetry_ExhaustsTransient(t *testing.T) {
	calls := 0
	probe := func(_ context.Context, target Target) Result {
		calls++
		return Result{TargetName: target.Name, Error: "dial tcp: i/o timeout"}
	}
	res := withUnlockRetry(probe)(context.Background(), Target{Name: "OpenAI", Kind: KindOpenAI})
	if res.Error == "" {
		t.Error("exhausted transient should keep error")
	}
	if calls != examTransientMaxRetries+1 {
		t.Errorf("probe calls = %d, want %d", calls, examTransientMaxRetries+1)
	}
}

// TestUnlockErrorStage_EmitsErrorRows 降级段:探测器构造失败时逐目标推 error 行,不静默跳过整段。
func TestUnlockErrorStage_EmitsErrorRows(t *testing.T) {
	targets := DefaultUnlockTargets()
	stage := unlockErrorStage(targets, io.EOF)
	if stage.name != "unlock" {
		t.Fatalf("stage name = %q, want unlock", stage.name)
	}

	var report ExamReport
	var rows, sectionDone int
	stage.run(context.Background(), func(e ExamEvent) {
		switch e.Phase {
		case "unlock":
			rows++
			if e.UnlockResult == nil || e.UnlockResult.Error == "" {
				t.Errorf("error row missing error: %+v", e)
			}
		case "section_done":
			sectionDone++
		}
	}, &report)

	if rows != len(targets) {
		t.Errorf("error rows = %d, want %d", rows, len(targets))
	}
	if sectionDone != 1 {
		t.Errorf("section_done = %d, want 1", sectionDone)
	}
}
