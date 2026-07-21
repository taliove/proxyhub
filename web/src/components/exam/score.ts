// 体检总分纯函数(与渲染解耦,便于单测)。
// 加权四项:稳定性 40% + 速度 25% + 解锁 20% + 出网质量 15% → 0-100 总分 + 五档 grade。
// 缺段降级:按有数据项归一化权重(权重和恒为 100%),UI 标注 partial 标记。
import type { ExamReport, ExamUnlockResult, ExamEgressMetrics } from '@/types'
import { isBaselineRow } from './examrows'

// 五档 grade:90+ 极好 / 75+ 良好 / 60+ 一般 / 40+ 较差 / <40 很差。
export type ExamGrade = 'excellent' | 'good' | 'fair' | 'poor' | 'very_poor'

// 速度评分对数映射锚点(基准下行 Mbps → 分数)。
const SPEED_ANCHORS: ReadonlyArray<{ mbps: number; score: number }> = [
  { mbps: 100, score: 100 },
  { mbps: 50, score: 85 },
  { mbps: 25, score: 70 },
  { mbps: 10, score: 50 },
  { mbps: 5, score: 30 },
  { mbps: 1, score: 10 }
]

// 单项得分明细(分数 + 权重)。
export interface ScoreItem {
  score: number | null // null 表示该段缺失
  weight: number // 0..1 归一化权重(缺段时为 0)
}

// 四项得分分解(稳定性 + 速度 + 解锁 + 出网)。
export interface ExamScoreBreakdown {
  stability: ScoreItem
  speed: ScoreItem
  unlock: ScoreItem
  egress: ScoreItem
}

// 总分结果(总分 + 档位 + 分解 + 是否部分数据)。
export interface ExamScoreResult {
  total: number // 0..100
  grade: ExamGrade
  breakdown: ExamScoreBreakdown
  partial: boolean // true 表示有段缺失,UI 需标注"部分数据"
}

// gradeFromScore 由 0-100 总分映射到五档。
export function gradeFromScore(score: number): ExamGrade {
  if (score >= 90) return 'excellent'
  if (score >= 75) return 'good'
  if (score >= 60) return 'fair'
  if (score >= 40) return 'poor'
  return 'very_poor'
}

// gradeLabel 档位中文标签。
export function gradeLabel(grade: ExamGrade): string {
  switch (grade) {
    case 'excellent':
      return '极好'
    case 'good':
      return '良好'
    case 'fair':
      return '一般'
    case 'poor':
      return '较差'
    case 'very_poor':
      return '很差'
  }
}

// gradeColorVar 档位对应的设计令牌变量名(随亮/暗主题变化)。
export function gradeColorVar(grade: ExamGrade): string {
  switch (grade) {
    case 'excellent':
      return '--ph-success'
    case 'good':
      return '--ph-success'
    case 'fair':
      return '--ph-warning'
    case 'poor':
      return '--ph-danger'
    case 'very_poor':
      return '--ph-danger'
  }
}

// calculateSpeedScore 速度评分:基准下行对数映射(锚点间线性插值)+ 上行 ±5 微调。
// 输入:基准行的 down_mbps 与可选 up_mbps(无基准行或失败返回 null,外层调用者处理)。
export function calculateSpeedScore(baseline: {
  down_mbps: number
  up_mbps?: number
}): number {
  const { down_mbps, up_mbps } = baseline
  // 低于最低锚点(1M)按 0 分计
  if (down_mbps < SPEED_ANCHORS[SPEED_ANCHORS.length - 1].mbps) return 0

  // 高于最高锚点(100M)封顶 100 分(上行微调也不超 100)
  if (down_mbps >= SPEED_ANCHORS[0].mbps) {
    const upBonus = up_mbps !== undefined ? upAdjust(up_mbps) : 0
    return Math.min(100, 100 + upBonus)
  }

  // 在锚点间线性插值
  for (let i = 0; i < SPEED_ANCHORS.length - 1; i++) {
    const upper = SPEED_ANCHORS[i]
    const lower = SPEED_ANCHORS[i + 1]
    if (down_mbps >= lower.mbps && down_mbps <= upper.mbps) {
      const ratio = (down_mbps - lower.mbps) / (upper.mbps - lower.mbps)
      const downScore = lower.score + ratio * (upper.score - lower.score)
      const upBonus = up_mbps !== undefined ? upAdjust(up_mbps) : 0
      return Math.max(0, Math.min(100, downScore + upBonus))
    }
  }

  // 回落(不应触及,锚点覆盖全域)
  return 0
}

// upAdjust 上行微调:≥50M +5 / ≤5M −5 / 中间 0。
function upAdjust(up_mbps: number): number {
  if (up_mbps >= 50) return 5
  if (up_mbps <= 5) return -5
  return 0
}

// calculateUnlockScore 解锁评分:6 目标 full=1/originals_only=0.5/其他=0 取均值 ×100。
export function calculateUnlockScore(results: ExamUnlockResult[]): number {
  if (results.length === 0) return 0
  let sum = 0
  for (const r of results) {
    if (r.level === 'full') sum += 1
    else if (r.level === 'originals_only') sum += 0.5
    // blocked/error/unknown 等均为 0
  }
  return (sum / results.length) * 100
}

// calculateEgressScore 出网质量评分:100 起扣(DNS 泄露 −30,机房 IP −15,无 IPv6 −10,保底 0)。
export function calculateEgressScore(egress: ExamEgressMetrics): number {
  let score = 100
  // DNS 泄露 −30
  if (egress.dns && !egress.dns.error && egress.dns.leak) score -= 30
  // 机房 IP(hosting=true)−15
  if (egress.ipv4 && !egress.ipv4.error && egress.ipv4.hosting) score -= 15
  // 无 IPv6 −10
  if (egress.ipv6 && !egress.ipv6.available) score -= 10
  return Math.max(0, score)
}

// calculateExamScore 汇总四项加权总分:稳定性 40% + 速度 25% + 解锁 20% + 出网 15%。
// 缺段降级:只按有数据项归一化权重(权重和恒为 100%),标记 partial=true。
export function calculateExamScore(report: ExamReport): ExamScoreResult {
  // 默认权重(稳定性 40% + 速度 25% + 解锁 20% + 出网 15%)
  const defaultWeights = { stability: 0.4, speed: 0.25, unlock: 0.2, egress: 0.15 }

  // 计算各项得分(缺失项为 null)
  const stabilityScore = report.stability ? report.stability.score : null
  const speedScore = extractSpeedScore(report)
  const unlockScore =
    report.unlock && report.unlock.results.length > 0
      ? calculateUnlockScore(report.unlock.results)
      : null
  const egressScore = report.egress ? calculateEgressScore(report.egress) : null

  // 归一化权重:只对有数据项分配权重
  let totalWeight = 0
  if (stabilityScore !== null) totalWeight += defaultWeights.stability
  if (speedScore !== null) totalWeight += defaultWeights.speed
  if (unlockScore !== null) totalWeight += defaultWeights.unlock
  if (egressScore !== null) totalWeight += defaultWeights.egress

  const partial = totalWeight < 0.999 // 有段缺失(浮点容差)

  // 归一化权重:如果权重和 <100%,按比例放大
  const normalize = totalWeight > 0 ? 1 / totalWeight : 0
  const weights = {
    stability: stabilityScore !== null ? defaultWeights.stability * normalize : 0,
    speed: speedScore !== null ? defaultWeights.speed * normalize : 0,
    unlock: unlockScore !== null ? defaultWeights.unlock * normalize : 0,
    egress: egressScore !== null ? defaultWeights.egress * normalize : 0
  }

  // 加权总分
  const total =
    (stabilityScore ?? 0) * weights.stability +
    (speedScore ?? 0) * weights.speed +
    (unlockScore ?? 0) * weights.unlock +
    (egressScore ?? 0) * weights.egress

  return {
    total: Math.max(0, Math.min(100, total)),
    grade: gradeFromScore(total),
    breakdown: {
      stability: { score: stabilityScore, weight: weights.stability },
      speed: { score: speedScore, weight: weights.speed },
      unlock: { score: unlockScore, weight: weights.unlock },
      egress: { score: egressScore, weight: weights.egress }
    },
    partial
  }
}

// extractSpeedScore 从 report 提取基准行并计算速度得分;无基准或失败返回 null。
function extractSpeedScore(report: ExamReport): number | null {
  const regions = report.region_speed?.regions ?? []
  const baseline = regions.find((r) => isBaselineRow(r))
  if (!baseline || baseline.error || !Number.isFinite(baseline.down_mbps)) return null
  return calculateSpeedScore({
    down_mbps: baseline.down_mbps,
    up_mbps: baseline.up_mbps
  })
}


