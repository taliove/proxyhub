// 体检历史与行摘要展示的纯计算逻辑(与渲染解耦,便于单测)。
import type { ExamHistoryEntry, ExamReport } from '@/types'
import { scoreLevel, type ScoreLevel } from './stability'

// parseTimeMs 解析后端 RFC3339 时间串为 epoch 毫秒;空/非法返回 NaN。
export function parseTimeMs(iso: string | undefined): number {
  if (!iso) return NaN
  return Date.parse(iso) // 非法串本身即返回 NaN
}

// relativeTimeZh 相对当前时刻的中文粗粒度描述。
// <1min 刚刚;<1h N分钟前;<24h N小时前;<7d N天前;更久回落到本地 MM-DD 日期。
// UI 全中文,故不用 "3h 前" 式混排(见 ticket 示例,取其意不取其形)。
export function relativeTimeZh(fromMs: number, nowMs: number = Date.now()): string {
  if (!Number.isFinite(fromMs)) return '—'
  const diff = nowMs - fromMs
  if (diff < 60_000) return '刚刚' // 含时钟偏差导致的“未来”样本
  const min = Math.floor(diff / 60_000)
  if (min < 60) return `${min}分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}小时前`
  const day = Math.floor(hr / 24)
  if (day < 7) return `${day}天前`
  const d = new Date(fromMs)
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${mm}-${dd}`
}

// examStabilityScore 读取报告稳定性分;无稳定性段返回 null。
export function examStabilityScore(report: ExamReport): number | null {
  return report.stability ? report.stability.score : null
}

// examUnlockSummary 解锁段摘要:"解锁 n/total"。
// 已解锁 = 流媒体 level==='full' 或(AI 类无 level 时)available===true;无结果返回空串。
export function examUnlockSummary(report: ExamReport): string {
  const results = report.unlock?.results ?? []
  if (results.length === 0) return ''
  const unlocked = results.filter((r) => (r.level ? r.level === 'full' : r.available)).length
  return `解锁 ${unlocked}/${results.length}`
}

// ExamBadge 节点行体检摘要徽标数据。
export interface ExamBadge {
  score: number
  level: ScoreLevel
  relative: string
  text: string // "稳定性 87 · 3小时前"
}

// examBadgeSummary 由最近一次体检记录生成行摘要徽标;无记录/无稳定性分返回 null(不占位)。
export function examBadgeSummary(entry: ExamHistoryEntry | null, nowMs?: number): ExamBadge | null {
  if (!entry) return null
  const score = examStabilityScore(entry.report)
  if (score === null) return null
  const relative = relativeTimeZh(parseTimeMs(entry.created_at), nowMs)
  return { score, level: scoreLevel(score), relative, text: `稳定性 ${score} · ${relative}` }
}

// ExamTimelineItem 抽屉体检历史时间线的一行视图模型。
export interface ExamTimelineItem {
  id: number
  createdAt: string
  relative: string
  score: number | null
  scoreLevel: ScoreLevel | null
  unlockSummary: string
}

// buildTimelineItems 把体检历史(后端已按时间倒序)映射为时间线视图行。
// 空历史返回空数组,驱动“尚未体检”引导态。
export function buildTimelineItems(
  entries: ExamHistoryEntry[],
  nowMs?: number
): ExamTimelineItem[] {
  return entries.map((e) => {
    const score = examStabilityScore(e.report)
    return {
      id: e.id,
      createdAt: e.created_at,
      relative: relativeTimeZh(parseTimeMs(e.created_at), nowMs),
      score,
      scoreLevel: score === null ? null : scoreLevel(score),
      unlockSummary: examUnlockSummary(e.report)
    }
  })
}
