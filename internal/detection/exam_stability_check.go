package detection

import (
	"context"
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// ExamStreamEgressStability 单节点"出网+稳定性"检查:复用体检出网段(IPv4/IPv6/DNS 画像,
// 语义与 ExamStream 出网段一致)与稳定性段(多次采样,0-100 评分),不含解锁目标、不测速
// ——即精简体检(ExamStreamSimplified)去掉基准下行的变体,逐段组合复用既有实现。
// 返回报告带 source=stability_check 来源标记:落 exam_history 时可与完整四段体检区分,
// "最近体检"消费方(节点列表稳定性分/总分/体检历史时间线)只认完整体检口径,不被
// 本检查的缺段报告抢占。
func (d *Detector) ExamStreamEgressStability(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
	cfg := d.resolveExamConfig()

	probe, err := d.stabilityProbeFactory(node)
	if err != nil {
		emit(ExamEvent{Phase: "error", Error: fmt.Sprintf("create proxy session: %v", err)})
		// 建会话失败:无稳定性段 -> 调用方不落历史(与体检语义一致);来源标记照常带出。
		return ExamReport{Source: ExamSourceStabilityCheck}
	}

	var stages []examStage

	// 第一段:出网信息(IPv4/IPv6/DNS 画像)。死节点 fail-fast(出网全失败由编排器短路后续段)。
	if eprobe, eerr := d.egressProbeFactory(node); eerr == nil {
		stages = append(stages, egressStage(withEgressRetry(eprobe), egressSegmentTimeout))
	} else {
		stages = append(stages, egressErrorStage(eerr))
	}

	// 第二段:稳定性采样(1Hz 探测 generate_204),独占会话。
	stages = append(stages, stabilityStage(cfg, realClock(), probe))

	// 无第三段(基准下行/多地域测速)与第四段(解锁):本动作只查"通不通、稳不稳"。

	orch := &ExamOrchestrator{stages: stages}
	report := orch.Run(ctx, emit)
	report.Source = ExamSourceStabilityCheck
	return report
}
