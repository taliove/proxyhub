package detection

import (
	"context"
	"fmt"
	"time"
)

// 体检出网信息段:把模块化的出网探测(见 egress.go)接入体检编排——逐类完成推一行 egress,
// 段末推 section_done + 聚合指标。出网段是 ProbeEgress 之外、复用同一探测/重试/泄露判定的调用方。

// egressStage 构造出网信息段:三类小请求并行探测,每完成一类推一行 egress,
// 段末按出口国家接线 DNS 泄露判定,推 section_done + 聚合指标。
func egressStage(probe EgressProbe, timeout time.Duration) examStage {
	return examStage{
		name: "egress",
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			ectx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			agg := aggregateEgress(ectx, probe, func(partial EgressMetrics) {
				pc := partial
				emit(ExamEvent{Phase: "egress", Section: "egress", Egress: &pc})
			})

			report.Egress = &agg
			ac := agg
			emit(ExamEvent{Phase: "section_done", Section: "egress", Egress: &ac})
		},
	}
}

// egressErrorStage 降级段:探测器构造失败(如无法建立节点会话)时,三类各推一行 error 而非静默跳过,
// 使段的缺失对用户可解释(段末仍推 section_done)。
func egressErrorStage(cause error) examStage {
	msg := fmt.Sprintf("出网信息探测初始化失败: %v", cause)
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{Error: msg} },
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false, Error: msg} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{Error: msg} },
	}
	return egressStage(probe, egressSegmentTimeout)
}
