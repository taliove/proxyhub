package detection

import (
	"encoding/json"
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

// TestDispatchTarget_Generic generic/空 kind 不被 dispatch 拦截,交回通用流程。
func TestDispatchTarget_Generic(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	for _, k := range []Kind{"", KindGeneric} {
		if _, handled := dispatchTarget(node, Target{Name: "connectivity", Kind: k}); handled {
			t.Errorf("dispatchTarget kind=%q handled=true, want false (generic must fall through)", k)
		}
	}
}

// TestDispatchTarget_SpecializedNotImplemented 专用 kind 返回未实现错误(骨架,由 02-04 填充)。
func TestDispatchTarget_SpecializedNotImplemented(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	res, handled := dispatchTarget(node, Target{Name: "Netflix", Kind: KindNetflix})
	if !handled {
		t.Fatal("dispatchTarget(netflix) handled=false, want true")
	}
	if res.Available {
		t.Error("specialized skeleton result Available=true, want false")
	}
	if res.NodeKey != node.NodeKey() || res.TargetName != "Netflix" {
		t.Errorf("result identity mismatch: %+v", res)
	}
	if !strings.Contains(res.Error, "not implemented") {
		t.Errorf("error = %q, want it to mention 'not implemented'", res.Error)
	}
}

// TestDispatchTarget_UnknownKind 未知 kind 明确报错,不得静默按 generic 处理。
func TestDispatchTarget_UnknownKind(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	res, handled := dispatchTarget(node, Target{Name: "Bogus", Kind: "spotify"})
	if !handled {
		t.Fatal("dispatchTarget(unknown) handled=false, want true (must not fall through to generic)")
	}
	if res.Available {
		t.Error("unknown kind result Available=true, want false")
	}
	if !strings.Contains(res.Error, "unknown") {
		t.Errorf("error = %q, want it to mention 'unknown'", res.Error)
	}
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
