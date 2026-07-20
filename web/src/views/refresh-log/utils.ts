import { InfoFilled, WarningFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import type { RefreshRun, RefreshEvent } from '@/types'

export interface EventGroup {
  stage: string
  events: RefreshEvent[]
}

// 错误信息提取:优先后端返回体,其次 message,最后兜底
export const errMessage = (e: unknown): string => {
  const err = e as { response?: { data?: string }; message?: string }
  return err?.response?.data || err?.message || '未知错误'
}

export const isConflict = (e: unknown): boolean =>
  (e as { response?: { status?: number } })?.response?.status === 409

export const triggerLabel = (t: string): string =>
  (({ manual: '手动', scheduled: '定时', startup: '启动' }) as Record<string, string>)[t] || t

export const statusTagType = (s: string): 'success' | 'warning' | 'danger' | 'primary' | 'info' =>
  (({ success: 'success', partial: 'warning', failed: 'danger', running: 'primary' }) as const)[
    s as 'success'
  ] || 'info'

export const statusLabel = (s: string): string =>
  (
    ({ success: '成功', partial: '部分', failed: '失败', running: '进行中' }) as Record<
      string,
      string
    >
  )[s] || s

export const stageLabel = (s: string): string =>
  (({ fetch: '拉取', check: '健康检查', filter: '过滤', done: '完成' }) as Record<string, string>)[
    s
  ] || s

export const levelIcon = (l: string) =>
  (
    ({ info: InfoFilled, warn: WarningFilled, error: CircleCloseFilled }) as Record<
      string,
      typeof InfoFilled
    >
  )[l] || InfoFilled

export const formatNodes = (row: RefreshRun): string => {
  if (row.status === 'failed' && row.total_nodes === 0) return '—'
  return `${row.total_nodes}/${row.available_nodes}/${row.final_nodes}`
}

export const formatTime = (iso: string): string => {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

// 事件按阶段分组,固定阶段顺序
export const groupedEvents = (events: RefreshEvent[]): EventGroup[] => {
  const map = new Map<string, RefreshEvent[]>()
  for (const e of events) {
    if (!map.has(e.stage)) map.set(e.stage, [])
    map.get(e.stage)!.push(e)
  }
  const order = ['fetch', 'check', 'filter', 'done']
  return order.filter((s) => map.has(s)).map((s) => ({ stage: s, events: map.get(s)! }))
}

// 按 event id 去重,返回新数组(不可变)
export const dedupeEvents = (
  existing: RefreshEvent[],
  incoming: RefreshEvent[]
): RefreshEvent[] => {
  const seen = new Set(existing.map((e) => e.id))
  const fresh = incoming.filter((e) => !seen.has(e.id))
  return [...existing, ...fresh]
}
