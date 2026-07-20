package detection

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// IsGeneric 报告该目标是否为通用判定目标(空/generic kind)。
// 聚合节点可用性时只采纳通用连通性目标的结果,不受专用解锁目标(可能未实现)影响。
func (t Target) IsGeneric() bool {
	return t.Kind == "" || t.Kind == KindGeneric
}

// resolveKind 规范化并校验 Target.Kind。
// 空值按 generic 处理;六种专用 kind 原样返回;其余一律报错(不得静默按 generic 处理)。
func (t Target) resolveKind() (Kind, error) {
	switch t.Kind {
	case "", KindGeneric:
		return KindGeneric, nil
	case KindNetflix, KindYouTubePremium, KindDisneyPlus, KindOpenAI, KindClaude, KindGemini:
		return t.Kind, nil
	default:
		return "", fmt.Errorf("unknown detection target kind %q", t.Kind)
	}
}

// dispatchTarget 按 kind 分发检测:
//   - generic/空 kind:返回 handled=false,交回调用方走通用状态码/关键字判定流程(行为零变化);
//   - 专用 kind:返回 handled=true 的"未实现"骨架结果,由 02-04 票据填充真实判定;
//   - 未知 kind:返回 handled=true 的明确错误结果,拒绝静默降级为 generic。
//
// 返回 handled=false 时第一个返回值为零值,调用方须忽略它。
func dispatchTarget(node *subscription.Node, target Target) (Result, bool) {
	base := Result{
		NodeKey:    node.NodeKey(),
		TargetName: target.Name,
		Available:  false,
	}

	kind, err := target.resolveKind()
	if err != nil {
		base.Error = err.Error()
		return base, true
	}
	if kind == KindGeneric {
		return Result{}, false
	}

	// 专用 kind 骨架:判定逻辑尚未实现(02-04)。
	base.Error = fmt.Sprintf("unlock detection for kind %q not implemented", kind)
	return base, true
}

// DefaultUnlockTargets 首次启动播种的六个解锁检测目标(每种专用 kind 一个)。
// URL/期望值为合理占位,专用判定逻辑落地时(02-04)可细化;此处只保证 kind 注册齐全。
func DefaultUnlockTargets() []Target {
	return []Target{
		{Name: "Netflix", Kind: KindNetflix, URL: "https://www.netflix.com/title/80018499", Method: "GET", ExpectStatus: []int{200}},
		{Name: "YouTube Premium", Kind: KindYouTubePremium, URL: "https://www.youtube.com/premium", Method: "GET", ExpectStatus: []int{200}},
		{Name: "Disney+", Kind: KindDisneyPlus, URL: "https://www.disneyplus.com", Method: "GET", ExpectStatus: []int{200}},
		{Name: "OpenAI", Kind: KindOpenAI, URL: "https://chat.openai.com", Method: "GET", ExpectStatus: []int{200}},
		{Name: "Claude", Kind: KindClaude, URL: "https://claude.ai", Method: "GET", ExpectStatus: []int{200}},
		{Name: "Gemini", Kind: KindGemini, URL: "https://gemini.google.com", Method: "GET", ExpectStatus: []int{200}},
	}
}
