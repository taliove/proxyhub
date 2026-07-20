package detection

import (
	"context"
	"fmt"
	"net/http"

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

// UnlockChecker 专用解锁判定函数(02-04 票据逐个实现)。
// client 已配置为经被测节点代理、超时由调用方设定;实现方填充完整 Result
// (含 Available/Level/Region/Latency/Error),NodeKey/TargetName 由实现方按 node/target 填。
type UnlockChecker func(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result

// unlockCheckers 专用 kind -> 判定器注册表。
// 各 unlock_*.go 在 init 中注册自己,新增判定器只加新文件、不改共享代码。
var unlockCheckers = map[Kind]UnlockChecker{}

// RegisterUnlockChecker 注册专用判定器。
// 对未知 kind 注册或重复注册直接 panic:这是编程错误,必须在启动/测试时立刻暴露。
func RegisterUnlockChecker(kind Kind, c UnlockChecker) {
	if _, err := (Target{Kind: kind}).resolveKind(); err != nil || kind == KindGeneric {
		panic(fmt.Sprintf("register unlock checker for invalid kind %q", kind))
	}
	if _, dup := unlockCheckers[kind]; dup {
		panic(fmt.Sprintf("duplicate unlock checker for kind %q", kind))
	}
	unlockCheckers[kind] = c
}

// specializedChecker 取专用 kind 的判定器;未注册(尚未实现)返回明确错误。
func specializedChecker(kind Kind) (UnlockChecker, error) {
	c, ok := unlockCheckers[kind]
	if !ok {
		return nil, fmt.Errorf("unlock detection for kind %q not implemented", kind)
	}
	return c, nil
}

// targetErrorResult 构造携带明确错误信息的判定结果(未知 kind / 未实现等框架级失败)。
func targetErrorResult(node *subscription.Node, target Target, msg string) Result {
	return Result{
		NodeKey:    node.NodeKey(),
		TargetName: target.Name,
		Available:  false,
		Error:      msg,
	}
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
