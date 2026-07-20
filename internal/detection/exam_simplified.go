package detection

import (
	"context"
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// ExamStreamSimplified 单节点精简体检(批量体检专用):复用既有组件,跑精简版体检。
// 精简版 = 出网信息 + 稳定性采样 + 基准下行,跳过多地域 8 区与解锁(解锁由批量检测覆盖)。
func (d *Detector) ExamStreamSimplified(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
	cfg := d.resolveExamConfig()

	probe, err := d.stabilityProbeFactory(node)
	if err != nil {
		emit(ExamEvent{Phase: "error", Error: fmt.Sprintf("create proxy session: %v", err)})
		return ExamReport{}
	}

	var stages []examStage

	// 第一段:出网信息(IPv4/IPv6/DNS),前置以便快速看到出口 IP/地区/DNS,死节点 fail-fast。
	if eprobe, eerr := d.egressProbeFactory(node); eerr == nil {
		stages = append(stages, egressStage(withEgressRetry(eprobe), egressSegmentTimeout))
	} else {
		stages = append(stages, egressErrorStage(eerr))
	}

	// 第二段:稳定性采样(1Hz 探测 generate_204),独占会话。
	stages = append(stages, stabilityStage(cfg, realClock(), probe))

	// 第三段:基准下行(Cloudflare 就近 POP 单行对照),跳过多地域 8 区测速。
	// 基准行足以判断带宽级别(个位 Mbps vs 百 Mbps),多地域 8 区在批量场景下墙钟过长。
	if rprobe, rerr := d.regionSpeedProbeFactory(node); rerr == nil {
		// 只测基准行(examRegionsWithBaseline 首元素),不测 8 区
		baselineRegion := examRegionsWithBaseline()[0]
		stages = append(stages, regionSpeedStage([]Region{baselineRegion}, withRegionRetry(rprobe)))
	} else {
		// 工厂失败时推降级段(单行 error)
		stages = append(stages, regionSpeedErrorStage([]Region{examRegionsWithBaseline()[0]}, rerr))
	}

	// 跳过第四段(解锁判定):批量体检用批量检测覆盖,不在此重复。

	orch := &ExamOrchestrator{stages: stages}
	return orch.Run(ctx, emit)
}
