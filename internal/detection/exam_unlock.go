package detection

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 解锁段默认参数。6 个小请求并行执行,整段共享一个总超时。
const unlockSegmentTimeoutSec = 15

// unlockSegmentTimeout 解锁段总超时:到点经 ctx 取消所有在飞判定,ctx-aware 的 checker 随即返回 error。
// 收集循环会等齐全部 probe,故这是协作式超时;硬兜底是生产 client 的 Timeout(见 defaultUnlockProbe)。
const unlockSegmentTimeout = unlockSegmentTimeoutSec * time.Second

// UnlockProbe 对单个解锁目标执行判定:经同一节点会话的 client + 注册表分发,复用批量检测的 checker。
type UnlockProbe func(ctx context.Context, target Target) Result

// UnlockMetrics 解锁段聚合结果(每个目标一条,顺序与 DefaultUnlockTargets 一致)。
type UnlockMetrics struct {
	Results []Result `json:"results"`
}

// runUnlockChecks 并行执行各目标判定,按完成顺序逐个回调 onResult(在调用方 goroutine 内串行触发,
// 保证 SSE emit 线程安全);返回值按目标原序落位,不受完成先后影响。ctx 取消由各 probe 自行响应。
func runUnlockChecks(ctx context.Context, targets []Target, probe UnlockProbe, onResult func(Result)) []Result {
	type indexed struct {
		i int
		r Result
	}
	ch := make(chan indexed, len(targets))
	for i, t := range targets {
		go func(i int, t Target) {
			ch <- indexed{i: i, r: probe(ctx, t)}
		}(i, t)
	}

	results := make([]Result, len(targets))
	for range targets {
		got := <-ch
		results[got.i] = got.r
		if onResult != nil {
			onResult(got.r)
		}
	}
	return results
}

// withUnlockRetry 包裹解锁探针,叠加网络类失败重试:传输抖动(Level 为空且 Error 为传输类)
// 重试至多 examTransientMaxRetries 次,仅返回最后一次结果;判定结论(full/blocked/originals_only,
// Level 非空)绝不重试,解析/配置类失败(非传输)也不重试。ctx 取消则不再重试。
// 重试在单次探针调用内同步完成,对 runUnlockChecks 透明,不改判定器本身(包在调用侧)。
func withUnlockRetry(probe UnlockProbe) UnlockProbe {
	return func(ctx context.Context, target Target) Result {
		return retryResult(ctx, examTransientMaxRetries,
			func() Result { return probe(ctx, target) },
			// 判定结论(Level 非空)绝不重试;仅传输抖动重试:结构化优先(cause),文本兜底(Error)。
			// 与出网/区域三段统一经 retryableTransient。
			func(res Result) bool { return res.Level == "" && retryableTransient(res.cause, res.Error) },
		)
	}
}

// unlockStage 构造解锁段:整段套一个总超时,并行判定各目标,每完成一个推 unlock 行,段末推 section_done + 指标。
func unlockStage(targets []Target, probe UnlockProbe, timeout time.Duration) examStage {
	return examStage{
		name: "unlock",
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			uctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			results := runUnlockChecks(uctx, targets, probe, func(r Result) {
				rc := r
				emit(ExamEvent{Phase: "unlock", Section: "unlock", UnlockResult: &rc})
			})

			metrics := UnlockMetrics{Results: results}
			report.Unlock = &metrics
			mc := metrics
			emit(ExamEvent{Phase: "section_done", Section: "unlock", Unlock: &mc})
		},
	}
}

// unlockErrorStage 降级段:探测器构造失败(如无法建立节点会话)时,逐目标推一行 error 而非静默跳过整段,
// 使段的缺失对用户可解释(段末仍推 section_done)。
func unlockErrorStage(targets []Target, cause error) examStage {
	msg := fmt.Sprintf("解锁检测初始化失败: %v", cause)
	probe := func(_ context.Context, t Target) Result {
		return Result{TargetName: t.Name, Available: false, Error: msg}
	}
	return unlockStage(targets, probe, unlockSegmentTimeout)
}

// newUnlockProbe 绑定一个 client 与 node,返回经注册表分发的解锁探测器:
// 与批量检测的 detectTarget 一样走 specializedChecker,判定器单份实现,体检链路不另起炉灶。
func newUnlockProbe(client *http.Client, node *subscription.Node) UnlockProbe {
	return func(ctx context.Context, target Target) Result {
		kind, err := target.resolveKind()
		if err != nil {
			return targetErrorResult(node, target, err.Error())
		}
		if kind == KindGeneric {
			return targetErrorResult(node, target, fmt.Sprintf("target %q is not an unlock kind", target.Name))
		}
		checker, err := specializedChecker(kind)
		if err != nil {
			return targetErrorResult(node, target, err.Error())
		}
		return checker(ctx, client, node, target)
	}
}

// defaultUnlockProbe 生产探测器:从 node 建一条 mihomo 会话(整段 6 个判定复用该 client 连接池),
// 判定经注册表分发。与稳定性/多地域段同构:每场体检独立会话,测试可注入假探测器绕过真实网络。
func (d *Detector) defaultUnlockProbe(node *subscription.Node) (UnlockProbe, error) {
	adapter, err := d.newProxyAdapter(node)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: adapter.DialContext},
		Timeout:   unlockSegmentTimeout,
	}
	return newUnlockProbe(client, node), nil
}
