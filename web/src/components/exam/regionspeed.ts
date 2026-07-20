// 多地域测速段展示的纯计算逻辑(与渲染解耦,便于单测)。
import type { ExamRegionResult } from '@/types'

export type RegionRowStatus = 'ok' | 'error'

// regionRowStatus 由结果是否带 error 判定单区状态。
export function regionRowStatus(r: ExamRegionResult): RegionRowStatus {
  return r.error ? 'error' : 'ok'
}

// regionStatusText 单区状态中文标签。
export function regionStatusText(r: ExamRegionResult): string {
  return regionRowStatus(r) === 'error' ? '失败' : '正常'
}

// formatTtfb 毫秒延迟展示(整数 + ms);非有限值显示占位。
export function formatTtfb(v: number | undefined): string {
  if (v === undefined || !Number.isFinite(v)) return '-'
  return `${Math.round(v)} ms`
}

// formatMbps 下行速率展示(一位小数 + Mbps);非有限值显示占位。
export function formatMbps(v: number | undefined): string {
  if (v === undefined || !Number.isFinite(v)) return '-'
  return `${v.toFixed(1)} Mbps`
}

// upsertRegionRow 以区域 code 为键插入/更新一行,返回新数组(不可变)。
// 首次到达追加;重跑体检时同 code 覆盖旧行,顺序保持首次出现的位置。
export function upsertRegionRow(
  rows: ExamRegionResult[],
  row: ExamRegionResult
): ExamRegionResult[] {
  const idx = rows.findIndex((r) => r.code === row.code)
  if (idx === -1) return [...rows, row]
  return rows.map((r, i) => (i === idx ? row : r))
}
