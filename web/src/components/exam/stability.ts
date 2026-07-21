// 稳定性报告展示的纯计算逻辑(与渲染解耦,便于单测)。
import type { ExamStabilitySample } from '@/types'

// 评分档位阈值:>=85 优,>=60 良,其余差。用于评分环/标签配色与文案。
export const SCORE_GOOD = 85
export const SCORE_FAIR = 60

export type ScoreLevel = 'good' | 'fair' | 'poor'

// scoreLevel 由 0-100 评分映射到三档。
export function scoreLevel(score: number): ScoreLevel {
  if (score >= SCORE_GOOD) return 'good'
  if (score >= SCORE_FAIR) return 'fair'
  return 'poor'
}

// scoreLabel 评分档位中文标签。
export function scoreLabel(score: number): string {
  switch (scoreLevel(score)) {
    case 'good':
      return '稳定'
    case 'fair':
      return '一般'
    default:
      return '不稳定'
  }
}

// scoreColorVar 评分档位对应的设计令牌变量名(随亮/暗主题变化)。
export function scoreColorVar(score: number): string {
  switch (scoreLevel(score)) {
    case 'good':
      return '--ph-success'
    case 'fair':
      return '--ph-warning'
    default:
      return '--ph-danger'
  }
}

// formatMs 毫秒延迟展示(整数 + ms);非有限值显示占位。
export function formatMs(v: number | undefined): string {
  if (v === undefined || !Number.isFinite(v)) return '-'
  return `${Math.round(v)} ms`
}

// formatLossPct 丢包率(0..1)转百分比字符串。
export function formatLossPct(rate: number | undefined): string {
  if (rate === undefined || !Number.isFinite(rate)) return '-'
  const pct = Math.max(0, Math.min(1, rate)) * 100
  // 保留一位小数,整数则去掉小数点
  return Number.isInteger(pct) ? `${pct}%` : `${pct.toFixed(1)}%`
}

// SparklinePoint 归一化到 viewBox 的坐标点。
export interface SparklinePoint {
  x: number
  y: number
}

// buildSparklinePoints 把稳定性采样序列映射为 sparkline 折线坐标(SVG viewBox 内)。
// 只画成功样本的延迟;丢包点跳过(不参与连线)。延迟按 [0, max] 归一到 [height, 0]。
// 空序列 / 单点做退化处理,保证渲染不崩。
// offsetX/offsetY: plot area offsets to reserve space for axis labels.
export function buildSparklinePoints(
  samples: ExamStabilitySample[],
  width: number,
  height: number,
  offsetX = 0,
  offsetY = 0
): SparklinePoint[] {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length === 0) return []

  const max = Math.max(...ok.map((s) => s.latency_ms), 1)
  const plotWidth = width - offsetX
  const plotHeight = height - offsetY
  const stepX = ok.length > 1 ? plotWidth / (ok.length - 1) : 0

  return ok.map((s, i) => {
    const x = offsetX + (ok.length > 1 ? i * stepX : plotWidth / 2)
    const y = offsetY + plotHeight - (s.latency_ms / max) * plotHeight
    return { x: round2(x), y: round2(y) }
  })
}

// buildSparklinePath 由坐标点生成 SVG polyline 的 points 属性字符串。
export function buildSparklinePath(points: SparklinePoint[]): string {
  return points.map((p) => `${p.x},${p.y}`).join(' ')
}

function round2(v: number): number {
  return Math.round(v * 100) / 100
}
