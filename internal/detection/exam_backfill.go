package detection

import (
	"context"
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// ExamSourceBackfill 「补齐信息」报告来源标记:出网画像 + 解锁 + 短采样稳定性的轻量报告
// (跳多地域测速与基准下行)。与 stability_check 不同,它参与"完整体检口径"消费方
// (节点列表稳定性分接受粗分,空杠比分不准更糟),但带标记供需要区分时使用。
const ExamSourceBackfill = "backfill"

// 补齐信息的短稳定性采样:5s 窗口(样本数 = 5s/间隔,默认 1Hz → 5 个样本),
// 粗评稳定性;完整采样口径留给「出网+稳定性」与深度体检。
const backfillStabilityDurationSec = 5

// ExamStreamBackfill 单节点「补齐信息」:尽快拿到全部列表字段,不求特别准确。
// 段组合 = 出网画像(egress,地区真值) + 解锁判定(6 目标) + 短稳定性采样;
// 明确跳过多地域 8 区测速与基准下行(这两段是完整四段的墙钟大头)。
// 报告带 source=backfill;connectivity/解锁写回 node_health 由 server 层 onComplete 负责。
func (d *Detector) ExamStreamBackfill(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
	cfg := d.resolveExamConfig()
	// 短采样:复制配置覆盖时长,不动全局
	cfg.StabilityDurationSec = backfillStabilityDurationSec

	probe, err := d.stabilityProbeFactory(node)
	if err != nil {
		emit(ExamEvent{Phase: "error", Error: fmt.Sprintf("create proxy session: %v", err)})
		// 建会话失败:无稳定性段 -> 调用方不落历史(与体检语义一致);来源标记照常带出。
		return ExamReport{Source: ExamSourceBackfill}
	}

	var stages []examStage

	// 第一段:出网信息(IPv4/IPv6/DNS 画像)。死节点 fail-fast(出网全失败短路后续段)。
	if eprobe, eerr := d.egressProbeFactory(node); eerr == nil {
		stages = append(stages, egressStage(withEgressRetry(eprobe), egressSegmentTimeout))
	} else {
		stages = append(stages, egressErrorStage(eerr))
	}

	// 第二段:短稳定性采样(粗评,独占会话)。
	stages = append(stages, stabilityStage(cfg, realClock(), probe))

	// 第三段:解锁判定(6 目标,与深度体检同段)。补齐信息要填解锁列,不能省。
	if uprobe, uerr := d.unlockProbeFactory(node); uerr == nil {
		stages = append(stages, unlockStage(DefaultUnlockTargets(), withUnlockRetry(uprobe), unlockSegmentTimeout))
	} else {
		stages = append(stages, unlockErrorStage(DefaultUnlockTargets(), uerr))
	}

	orch := &ExamOrchestrator{stages: stages}
	report := orch.Run(ctx, emit)
	report.Source = ExamSourceBackfill
	return report
}
