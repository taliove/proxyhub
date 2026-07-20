// 任务中心纯逻辑:kind 中文名映射、状态展示元数据、游标进度解析。
// 无框架依赖,便于 vitest 覆盖。
import type { JobStatus } from '@/api/jobs'

// KIND_LABELS 任务 kind -> 中文名。未知 kind 回落到原始值(见 kindLabel)。
const KIND_LABELS: Record<string, string> = {
  exam: '单节点体检',
  batch_exam: '批量体检',
  batch_detection: '批量解锁检测',
  retag_all: '晚间标签重算'
}

// kindLabel 返回 kind 的中文名;未知 kind 原样返回(不掩盖新类型)。
export function kindLabel(kind: string): string {
  return KIND_LABELS[kind] || kind
}

// StatusMeta 状态展示元数据:中文标签 + el-tag 语义色 + 是否运行中。
export interface StatusMeta {
  label: string
  tag: 'success' | 'warning' | 'danger' | 'info' | 'primary'
  running: boolean
}

// STATUS_META 每个状态的展示元数据。
const STATUS_META: Record<JobStatus, StatusMeta> = {
  running: { label: '运行中', tag: 'primary', running: true },
  done: { label: '已完成', tag: 'success', running: false },
  failed: { label: '失败', tag: 'danger', running: false },
  cancelled: { label: '已取消', tag: 'info', running: false },
  interrupted: { label: '已中断', tag: 'warning', running: false }
}

// statusMeta 返回状态元数据;未知状态回落到 info + 原始值。
export function statusMeta(status: string): StatusMeta {
  return STATUS_META[status as JobStatus] || { label: status, tag: 'info', running: false }
}

// isRunning 是否运行中(可取消 / 需轮询)。
export function isRunning(status: string): boolean {
  return statusMeta(status).running
}

// parseProgress 解析游标进度。
// 可续跑任务(batch_exam/batch_detection/retag_all)的 cursor 是已完成数的字符串。
// 总量不在 jobs 表中,故只能给出已完成计数;total 已知时(前端启动批量体检时缓存)拼成 "x/N"。
export function parseProgress(cursor: string | undefined, total?: number): string {
  const done = parseCursor(cursor)
  if (done === null) return '-'
  if (total && total > 0) return `${done}/${total}`
  return `已处理 ${done}`
}

// parseCursor 把 cursor 解析为非负整数;非法/空返回 null。
export function parseCursor(cursor: string | undefined): number | null {
  if (!cursor) return null
  const n = Number(cursor)
  if (!Number.isInteger(n) || n < 0) return null
  return n
}
