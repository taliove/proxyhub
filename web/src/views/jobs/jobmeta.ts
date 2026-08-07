// 任务中心纯逻辑:kind 中文名映射、状态展示元数据、游标进度解析。
// 无框架依赖,便于 vitest 覆盖。
import type { JobStatus } from '@/api/jobs'
import { parseAirportTestCursor } from '@/composables/useAirportTest'

// KIND_LABELS 任务 kind -> 中文名。未知 kind 回落到原始值(见 kindLabel)。
// 检查动作词汇统一(见 CONTEXT「检查动作」):batch_detection = 出网快速检测,
// batch_stability = 出网+稳定性,batch_speedtest = 快速测速,batch_exam = 深度体检;
// 单节点对应 exam(深度体检)/ exam_stability(出网+稳定性)。
const KIND_LABELS: Record<string, string> = {
  exam: '深度体检',
  exam_stability: '出网+稳定性',
  batch_exam: '深度体检',
  batch_detection: '出网快速检测',
  batch_stability: '出网+稳定性',
  batch_speedtest: '快速测速',
  retag_all: '晚间标签重算',
  refresh: '刷新',
  airport_test: '机场测试'
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

// AIRPORT_TEST_PHASE_LABELS 机场测试 cursor 阶段 -> 中文标签。
const AIRPORT_TEST_PHASE_LABELS: Record<string, string> = {
  diagnosing: '诊断中',
  checking: '检活中',
  scoring: '评分中'
}

// parseProgress 解析游标进度。
// 可续跑任务(batch_exam/batch_detection/batch_stability/batch_speedtest/retag_all)的
// cursor 是已完成数的字符串。
// 总量不在 jobs 表中,故只能给出已完成计数;total 已知时(前端启动批量体检时缓存)拼成 "x/N"。
// airport_test 的 cursor 是 JSON {"phase","checked","total"}(ADR 0027 主进度源):
// 检活阶段显示 "检活 x/N",其余阶段显示阶段名。
export function parseProgress(cursor: string | undefined, total?: number): string {
  const done = parseCursor(cursor)
  if (done !== null) {
    if (total && total > 0) return `${done}/${total}`
    return `已处理 ${done}`
  }
  const c = parseAirportTestCursor(cursor)
  if (!c) return '-'
  if (c.phase === 'checking' && c.total > 0) return `检活 ${c.checked}/${c.total}`
  return AIRPORT_TEST_PHASE_LABELS[c.phase] || '进行中'
}

// parseCursor 把 cursor 解析为非负整数;非法/空返回 null。
export function parseCursor(cursor: string | undefined): number | null {
  if (!cursor) return null
  const n = Number(cursor)
  if (!Number.isInteger(n) || n < 0) return null
  return n
}

// JobParams 任务启动参数(jobs 表 params_json 的已知子集)。
export interface JobParams {
  node_keys?: string[]
  scope?: string // "all" / "query" / "selected"(2026-07 起写入；旧任务无此字段)
  trigger?: string // refresh kind: manual / scheduled / startup
  airport_id?: number // refresh 单机场
  airport_name?: string // refresh 单机场（展示用）
}

// parseJobParams 解析 params JSON 串;空/非法返回 null。
export function parseJobParams(params: string | undefined): JobParams | null {
  if (!params) return null
  try {
    const p = JSON.parse(params) as JobParams | null
    return p && typeof p === 'object' ? p : null
  } catch {
    return null
  }
}

// scopeLabel 生成任务范围的可读标识(替代裸 key 展示)。
// batch 类任务:优先读 params.scope;旧任务(无 scope)回退 node_keys 长度启发式——
// 空列表推定"全部节点",非空只说"N 个节点"(不妄断"选中",exam 全量会展开为完整列表)。
export function scopeLabel(job: { kind: string; key: string; params?: string }): string {
  switch (job.kind) {
    case 'batch_detection':
    case 'batch_stability':
    case 'batch_speedtest':
    case 'batch_exam': {
      const p = parseJobParams(job.params)
      const n = p?.node_keys?.length ?? 0
      if (p?.scope === 'all') return '全部节点'
      if (p?.scope === 'query') return `筛选结果 ${n} 个节点`
      if (p?.scope === 'selected') return `选中 ${n} 个节点`
      // 旧任务无 scope 标记:keys 为空推定全量,非空只报数量
      return n === 0 ? '全部节点' : `${n} 个节点`
    }
    case 'retag_all':
      return '全部节点'
    case 'refresh': {
      // 刷新:全量 key=all -> 全部机场;单机场 key=airport-<id> -> 机场名(params 尽力填充)
      if (job.key === 'all') return '全部机场'
      const p = parseJobParams(job.params)
      return p?.airport_name ? `单机场「${p.airport_name}」` : `单机场 ${job.key}`
    }
    case 'airport_test': {
      // 机场测试:key=airport-<id>(ADR 0027),params 带 airport_name(展示用)
      const p = parseJobParams(job.params)
      return p?.airport_name ? `单机场「${p.airport_name}」` : `单机场 ${job.key}`
    }
    default:
      return job.key
  }
}

// TRIGGER_LABELS 触发来源 -> 中文标签(refresh kind 的 params.trigger;
// 其他 kind 都是用户手动发起,一律归"手动")。
const TRIGGER_LABELS: Record<string, string> = {
  manual: '手动',
  scheduled: '定时',
  startup: '启动'
}

// jobTrigger 归一化任务来源标签;refresh 读 params.trigger,retag_all 是晚间
// 定时调度固定归"定时",其余 kind 都是用户手动发起归"手动"。
export function jobTrigger(job: { kind: string; params?: string }): string {
  if (job.kind === 'retag_all') return '定时'
  if (job.kind !== 'refresh') return '手动'
  const t = parseJobParams(job.params)?.trigger
  return (t && TRIGGER_LABELS[t]) || '手动'
}
