package detection

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestKindConstants 六种解锁 kind 常量齐全且取值稳定(与存储/前端契约一致)。
func TestKindConstants(t *testing.T) {
	want := map[Kind]string{
		KindGeneric:        "generic",
		KindNetflix:        "netflix",
		KindYouTubePremium: "youtube_premium",
		KindDisneyPlus:     "disney_plus",
		KindOpenAI:         "openai",
		KindClaude:         "claude",
		KindGemini:         "gemini",
	}
	for k, v := range want {
		if string(k) != v {
			t.Errorf("kind %q constant = %q, want %q", v, string(k), v)
		}
	}
}

// TestResolveKind 空值按 generic 处理;专用 kind 原样返回;未知 kind 明确报错。
func TestResolveKind(t *testing.T) {
	generic := []Kind{"", KindGeneric}
	for _, k := range generic {
		got, err := (Target{Kind: k}).resolveKind()
		if err != nil {
			t.Errorf("resolveKind(%q) unexpected error: %v", k, err)
		}
		if got != KindGeneric {
			t.Errorf("resolveKind(%q) = %q, want generic", k, got)
		}
	}

	specialized := []Kind{KindNetflix, KindYouTubePremium, KindDisneyPlus, KindOpenAI, KindClaude, KindGemini}
	for _, k := range specialized {
		got, err := (Target{Kind: k}).resolveKind()
		if err != nil {
			t.Errorf("resolveKind(%q) unexpected error: %v", k, err)
		}
		if got != k {
			t.Errorf("resolveKind(%q) = %q, want %q", k, got, k)
		}
	}

	if _, err := (Target{Kind: "spotify"}).resolveKind(); err == nil {
		t.Error("resolveKind(unknown) expected error, got nil")
	}
}

// TestSpecializedChecker_NotImplemented 未注册的专用 kind 返回"未实现"错误(由 02-04 逐个注册填充)。
func TestSpecializedChecker_NotImplemented(t *testing.T) {
	for _, k := range []Kind{KindNetflix, KindYouTubePremium, KindDisneyPlus, KindOpenAI, KindClaude, KindGemini} {
		if _, ok := unlockCheckers[k]; ok {
			continue // 已注册的 kind 跳过(后续票据落地后此测试自然收窄)
		}
		if _, err := specializedChecker(k); err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Errorf("specializedChecker(%q) error = %v, want it to mention 'not implemented'", k, err)
		}
	}
}

// TestRegisterUnlockChecker_Panics 对未知/generic kind 注册或重复注册必须 panic(编程错误,立即暴露)。
func TestRegisterUnlockChecker_Panics(t *testing.T) {
	noop := func(context.Context, *http.Client, *subscription.Node, Target) Result { return Result{} }

	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s expected panic, got none", name)
			}
		}()
		f()
	}

	mustPanic("unknown kind", func() { RegisterUnlockChecker("spotify", noop) })
	mustPanic("generic kind", func() { RegisterUnlockChecker(KindGeneric, noop) })

	// 重复注册:注册一次后再次注册同一 kind 必须 panic;测试后清理,不污染注册表。
	const k = KindNetflix
	if _, ok := unlockCheckers[k]; ok {
		t.Skipf("kind %q already registered by an init (post-02), duplicate case covered by init-time panic", k)
	}
	RegisterUnlockChecker(k, noop)
	defer delete(unlockCheckers, k)
	mustPanic("duplicate kind", func() { RegisterUnlockChecker(k, noop) })
}

// TestGenericTargetJSON_NoKindField 回归:generic(空 kind)Target 序列化不出现 kind 字段,API 响应零变化。
func TestGenericTargetJSON_NoKindField(t *testing.T) {
	data, err := json.Marshal(Target{
		Name:         "connectivity",
		URL:          "http://www.gstatic.com/generate_204",
		Method:       "GET",
		ExpectStatus: []int{204},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "\"kind\"") {
		t.Errorf("generic Target JSON must not contain kind field: %s", data)
	}
}

// TestGenericResultJSON_NoLevelRegion 回归:generic Result 序列化不出现 level/region 字段。
func TestGenericResultJSON_NoLevelRegion(t *testing.T) {
	data, err := json.Marshal(Result{
		NodeKey:    "example.com:443",
		TargetName: "connectivity",
		Available:  true,
		Latency:    50,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "\"level\"") {
		t.Errorf("generic Result JSON must not contain level field: %s", s)
	}
	if strings.Contains(s, "\"region\"") {
		t.Errorf("generic Result JSON must not contain region field: %s", s)
	}
}

// TestDefaultUnlockTargets 播种默认解锁目标覆盖六种专用 kind,且都非 generic。
func TestDefaultUnlockTargets(t *testing.T) {
	targets := DefaultUnlockTargets()
	got := make(map[Kind]bool)
	for _, tg := range targets {
		if tg.Kind == "" || tg.Kind == KindGeneric {
			t.Errorf("seeded target %q has generic/empty kind, want specialized", tg.Name)
		}
		if _, err := tg.resolveKind(); err != nil {
			t.Errorf("seeded target %q has invalid kind %q: %v", tg.Name, tg.Kind, err)
		}
		got[tg.Kind] = true
	}
	for _, want := range []Kind{KindNetflix, KindYouTubePremium, KindDisneyPlus, KindOpenAI, KindClaude, KindGemini} {
		if !got[want] {
			t.Errorf("DefaultUnlockTargets missing kind %q", want)
		}
	}
	if len(targets) != 6 {
		t.Errorf("DefaultUnlockTargets len = %d, want 6", len(targets))
	}
}
