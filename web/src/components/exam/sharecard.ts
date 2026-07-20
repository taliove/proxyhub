// 分享卡的纯计算逻辑(与渲染解耦,便于单测)。
// 只从 ExamReport 派生「可公开」字段:打码节点名、评分、基准下行、多地域最佳/最差、
// 解锁 6 宫格、出口地区 + DNS 泄露状态。刻意不派生任何 IP/UUID/服务器地址类字段。
import type { ExamReport, ExamRegionResult, ExamUnlockResult } from '@/types'
import { isBaselineRow, EXAM_UNLOCK_SLOTS } from './examrows'
import { unlockLevel, unlockLabel, type UnlockLevel } from './unlock'
import { ipv4Location } from './egress'

export const MASK = '***'
export const UNNAMED = '未命名节点'

// 打码分隔符:按常见节点命名切段。刻意不含 '.',避免把 IP/域名切碎后反而暴露片段。
const NAME_DELIMS = /[-_/|·\s@:]+/

// maskNodeName 把节点名打码:保留可辨识前缀,尾段(常含主机/IP/序号)以 *** 遮蔽。
// 多段(>=2):保留前 1~2 段 + '-***'(如 233boy-grpc-1.2.3.4 -> 233boy-grpc-***);
// 单段:仅保留至多 2 个字符 + '***';空名回落占位。
export function maskNodeName(name: string): string {
  const trimmed = (name ?? '').trim()
  if (trimmed === '') return UNNAMED
  const segs = trimmed.split(NAME_DELIMS).filter((s) => s !== '')
  if (segs.length >= 2) {
    const keep = Math.min(2, segs.length - 1)
    return `${segs.slice(0, keep).join('-')}-${MASK}`
  }
  const head = trimmed.slice(0, Math.max(1, Math.min(2, trimmed.length - 1)))
  return `${head}${MASK}`
}

// displayNodeName 依打码开关给出卡片展示名(打码默认开)。
export function displayNodeName(name: string, masked: boolean): string {
  if (masked) return maskNodeName(name)
  const trimmed = (name ?? '').trim()
  return trimmed === '' ? UNNAMED : trimmed
}

// formatExamTime 体检时间:本地 YYYY-MM-DD HH:mm;无效/缺失回落 '—'。
export function formatExamTime(t: string | number | Date | undefined): string {
  if (t === undefined || t === '') return '—'
  const d = t instanceof Date ? t : new Date(t)
  const ms = d.getTime()
  if (!Number.isFinite(ms)) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// shareScore 稳定性评分;无稳定性段返回 null(评分环不渲染)。
export function shareScore(report: ExamReport): number | null {
  return report.stability ? report.stability.score : null
}

// shareBaselineMbps 基准(Cloudflare 最近 POP)下行速率;无基准或失败返回 null。
export function shareBaselineMbps(report: ExamReport): number | null {
  const regions = report.region_speed?.regions ?? []
  const baseline = regions.find((r) => isBaselineRow(r))
  if (!baseline || baseline.error) return null
  return Number.isFinite(baseline.down_mbps) ? baseline.down_mbps : null
}

// RegionExtreme 多地域极值行(仅展示区域名 + 速率/延迟,无任何地址)。
export interface RegionExtreme {
  name: string
  down_mbps: number
  ttfb_ms: number
}

// shareRegionExtremes 非基准且成功的区域中:下行最高=最佳,最低=最差;不足则对应项为 null。
export function shareRegionExtremes(report: ExamReport): {
  best: RegionExtreme | null
  worst: RegionExtreme | null
} {
  const regions = (report.region_speed?.regions ?? []).filter(
    (r) => !isBaselineRow(r) && !r.error && Number.isFinite(r.down_mbps)
  )
  if (regions.length === 0) return { best: null, worst: null }
  let best = regions[0]
  let worst = regions[0]
  for (const r of regions) {
    if (r.down_mbps > best.down_mbps) best = r
    if (r.down_mbps < worst.down_mbps) worst = r
  }
  const pick = (r: ExamRegionResult): RegionExtreme => ({
    name: r.name,
    down_mbps: r.down_mbps,
    ttfb_ms: r.ttfb_ms
  })
  return { best: pick(best), worst: pick(worst) }
}

// UnlockCell 解锁 6 宫格单元(名称 + 三档 level + 中文标签)。
export interface UnlockCell {
  name: string
  level: UnlockLevel
  label: string
}

// shareUnlockCells 6 宫格解锁:按固定槽位归位;缺失项为 unknown/'未测'。
export function shareUnlockCells(report: ExamReport): UnlockCell[] {
  const byName = new Map<string, ExamUnlockResult>()
  for (const r of report.unlock?.results ?? []) byName.set(r.target_name, r)
  return EXAM_UNLOCK_SLOTS.map((name) => {
    const r = byName.get(name)
    if (!r) return { name, level: 'unknown', label: '未测' }
    return { name, level: unlockLevel(r), label: unlockLabel(r) }
  })
}

// unlockLevelColorVar 三档解锁 level 对应设计令牌(随亮/暗主题):full 绿 / originals_only 黄 / blocked 红 / unknown 中性。
export function unlockLevelColorVar(level: UnlockLevel): string {
  switch (level) {
    case 'full':
      return '--ph-success'
    case 'originals_only':
      return '--ph-warning'
    case 'blocked':
      return '--ph-danger'
    default:
      return '--ph-text-secondary'
  }
}

// LeakStatus DNS 泄露三态:未泄露 ok / 疑似泄露 leak / 未知(缺失或探测异常)unknown。
export type LeakStatus = 'ok' | 'leak' | 'unknown'

// EgressSummary 出口摘要(只含地区与 DNS 泄露状态,绝不含出口 IP 地址)。
export interface EgressSummary {
  ipv4Region: string // 国家·省·市;失败或缺失为空串
  dnsLeak: LeakStatus
}

// shareEgressSummary 出口摘要:IPv4 出口「地区」+ DNS 泄露状态。
// 安全契约:只取地区文案(ipv4Location),绝不返回 ipv4.ip / resolver_ip 等地址字段。
export function shareEgressSummary(report: ExamReport): EgressSummary {
  const e = report.egress
  const ipv4 = e?.ipv4
  const ipv4Region = ipv4 && !ipv4.error ? ipv4Location(ipv4) : ''
  const dns = e?.dns
  let dnsLeak: LeakStatus = 'unknown'
  if (dns && !dns.error) dnsLeak = dns.leak ? 'leak' : 'ok'
  return { ipv4Region, dnsLeak }
}

// leakColorVar DNS 泄露状态色:未泄露 绿 / 疑似泄露 红 / 未知 中性。
export function leakColorVar(status: LeakStatus): string {
  switch (status) {
    case 'ok':
      return '--ph-success'
    case 'leak':
      return '--ph-danger'
    default:
      return '--ph-text-secondary'
  }
}

// leakLabel DNS 泄露状态中文标签。
export function leakLabel(status: LeakStatus): string {
  switch (status) {
    case 'ok':
      return '未泄露'
    case 'leak':
      return '疑似泄露'
    default:
      return '未知'
  }
}
