// 一屏化体检的占位行模型(与渲染解耦,便于单测)。
// 三段(多地域/解锁/出网)从对话框打开即全部占位列出;数据到达即填值;
// 正在处理的行标记 active(脉冲高亮),其余未到达为 waiting,已结算为 ok/error。
// 到达顺序不做任何假设(契约:失败行可能晚到 = 重试中);行位置由固定槽位决定,不随到达顺序变动。
import type { ExamRegionResult, ExamUnlockResult, ExamEgressMetrics } from '@/types'

// 单行状态:waiting 未到达 / active 正在处理(高亮)/ ok 已结算 / error 已结算且失败。
export type RowStatus = 'waiting' | 'active' | 'ok' | 'error'

// 基准行(Cloudflare 最近 POP)契约:多地域段首行 name==='基准',置顶且样式可区分。
export const BASELINE_KEY = '__baseline__'
export const BASELINE_NAME = '基准'

// 8 个固定测速区域槽位(code 与后端 examRegions 一致,顺序即展示顺序)。
export const EXAM_REGION_SLOTS: ReadonlyArray<{ code: string; name: string }> = [
  { code: 'us_west', name: '美西' },
  { code: 'us_east', name: '美东' },
  { code: 'eu_frankfurt', name: '法兰克福' },
  { code: 'sg', name: '新加坡' },
  { code: 'jp_tokyo', name: '东京' },
  { code: 'au_sydney', name: '悉尼' },
  { code: 'ca_toronto', name: '多伦多' },
  { code: 'in_mumbai', name: '孟买' }
]

// 6 个固定解锁目标槽位(与后端 DefaultUnlockTargets 顺序一致)。
export const EXAM_UNLOCK_SLOTS: ReadonlyArray<string> = [
  'Netflix',
  'YouTube Premium',
  'Disney+',
  'OpenAI',
  'Claude',
  'Gemini'
]

export type EgressKind = 'ipv4' | 'ipv6' | 'dns'

// 3 个固定出网信息槽位。
export const EXAM_EGRESS_SLOTS: ReadonlyArray<{ kind: EgressKind; label: string }> = [
  { kind: 'ipv4', label: 'IPv4 出口' },
  { kind: 'ipv6', label: 'IPv6 出口' },
  { kind: 'dns', label: '出口 DNS' }
]

export interface RegionRow {
  key: string
  name: string
  baseline: boolean
  status: RowStatus
  result: ExamRegionResult | null
}

export interface UnlockRow {
  name: string
  status: RowStatus
  result: ExamUnlockResult | null
}

export interface EgressRow {
  kind: EgressKind
  label: string
  status: RowStatus
}

// isBaselineRow 判定一条测速结果是否为基准行(契约:仅凭 name==='基准',不依赖 code)。
export function isBaselineRow(r: ExamRegionResult): boolean {
  return (r.name ?? '').trim() === BASELINE_NAME
}

// regionSlotKey 到达行归属的槽位键:基准归基准槽,其余按 code。
function regionSlotKey(r: ExamRegionResult): string {
  return isBaselineRow(r) ? BASELINE_KEY : r.code
}

// applyActive 把有序行里第一条 waiting 提升为 active(仅当该段进行中)。返回新数组(不可变)。
// 「第一条 waiting」的语义 = 当前正在处理项:串行段里前序均已结算,后续尚未到达。
function applyActive<T extends { status: RowStatus }>(rows: T[], sectionActive: boolean): T[] {
  if (!sectionActive) return rows
  const idx = rows.findIndex((r) => r.status === 'waiting')
  if (idx === -1) return rows
  return rows.map((r, i) => (i === idx ? { ...r, status: 'active' } : r))
}

function settledStatus(hasError: boolean): RowStatus {
  return hasError ? 'error' : 'ok'
}

function regionStatus(r: ExamRegionResult | null): RowStatus {
  return r ? settledStatus(!!r.error) : 'waiting'
}

// buildRegionRows 生成 9 行(基准置顶 + 8 固定区域)占位合并结果。
// arrived 为已到达行(容忍乱序、可含基准);sectionActive 为多地域段是否进行中。
export function buildRegionRows(arrived: ExamRegionResult[], sectionActive: boolean): RegionRow[] {
  const byKey = new Map<string, ExamRegionResult>()
  for (const r of arrived) byKey.set(regionSlotKey(r), r)

  const baselineResult = byKey.get(BASELINE_KEY) ?? null
  const baselineRow: RegionRow = {
    key: BASELINE_KEY,
    name: BASELINE_NAME,
    baseline: true,
    result: baselineResult,
    status: regionStatus(baselineResult)
  }
  const fixedRows: RegionRow[] = EXAM_REGION_SLOTS.map((slot) => {
    const result = byKey.get(slot.code) ?? null
    return {
      key: slot.code,
      name: result?.name ?? slot.name,
      baseline: false,
      result,
      status: regionStatus(result)
    }
  })
  return applyActive([baselineRow, ...fixedRows], sectionActive)
}

// regionSectionComplete 8 个固定区域是否全部到达(与基准无关:基准可能缺席或晚到,
// 不阻塞后续解锁/出网段的进行中判定)。
export function regionSectionComplete(arrived: ExamRegionResult[]): boolean {
  const codes = new Set(arrived.filter((r) => !isBaselineRow(r)).map((r) => r.code))
  return EXAM_REGION_SLOTS.every((slot) => codes.has(slot.code))
}

function unlockStatus(r: ExamUnlockResult | null): RowStatus {
  return r ? settledStatus(!!r.error) : 'waiting'
}

// buildUnlockRows 生成 6 行解锁占位合并结果(按 target_name 归位)。
export function buildUnlockRows(arrived: ExamUnlockResult[], sectionActive: boolean): UnlockRow[] {
  const byName = new Map<string, ExamUnlockResult>()
  for (const r of arrived) byName.set(r.target_name, r)
  const rows: UnlockRow[] = EXAM_UNLOCK_SLOTS.map((name) => {
    const result = byName.get(name) ?? null
    return { name, result, status: unlockStatus(result) }
  })
  return applyActive(rows, sectionActive)
}

function egressStatus(egress: ExamEgressMetrics | null, kind: EgressKind): RowStatus {
  const item = egress?.[kind]
  if (item == null) return 'waiting'
  return settledStatus(!!(item as { error?: string }).error)
}

// buildEgressRows 生成 3 行出网占位合并结果(IPv4/IPv6/DNS)。
export function buildEgressRows(
  egress: ExamEgressMetrics | null,
  sectionActive: boolean
): EgressRow[] {
  const rows: EgressRow[] = EXAM_EGRESS_SLOTS.map((slot) => ({
    kind: slot.kind,
    label: slot.label,
    status: egressStatus(egress, slot.kind)
  }))
  return applyActive(rows, sectionActive)
}
