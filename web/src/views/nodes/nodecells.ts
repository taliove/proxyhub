import type { ExamHistoryEntry, ExamReport, Node } from '@/types'
import {
  examBadgeSummary,
  parseTimeMs,
  relativeTimeZh,
  type ExamBadge
} from '@/components/exam/examhistory'
import { isGenericTarget } from './unlock'
import type { UnifiedNode } from './selfmerge'
import { tagLabel } from '@/utils/taglabels'

// 统一节点表的单元格视图模型:与渲染解耦的纯计算,便于单测。

// 名称单元:标准名优先,原始名作副标题(仅在与标准名不同时显示)。
export interface NameCell {
  primary: string
  secondary: string
}
export const nameCell = (n: Node): NameCell => {
  const primary = n.display_name || n.name
  const secondary = n.display_name && n.display_name !== n.name ? n.name : ''
  return { primary, secondary }
}

// 列收敛后去掉独立"状态"列:状态降为名称旁的小标签。
// "不可用"不再出标签,由延迟列的状态色点(healthTone)表达,避免同一状态两处重复呈现。
export type StateTone = 'info' | 'warning' | 'danger'
export interface StateTag {
  label: string
  tone: StateTone
}
export const stateTags = (n: UnifiedNode): StateTag[] => {
  const tags: StateTag[] = []
  if (n.blocked) tags.push({ label: '已屏蔽', tone: 'warning' })
  if (n.stale) tags.push({ label: '已下架', tone: 'info' })
  if (n.enabled === false) tags.push({ label: '禁用', tone: 'info' })
  return tags
}

// 健康状态色点(延迟列):禁用/未测为灰点,不可用为红点,可用且有延迟为绿点。
export type HealthTone = 'success' | 'danger' | 'muted'
export const healthTone = (n: UnifiedNode): HealthTone => {
  if (n.enabled === false) return 'muted'
  if (!n.available) return 'danger'
  return n.latency > 0 ? 'success' : 'muted'
}
export const healthLabel = (n: UnifiedNode): string => {
  if (n.enabled === false) return '已禁用'
  if (!n.available) return '不可用'
  return n.latency > 0 ? '可用' : '未检测'
}

// 延迟文案:无有效延迟(禁用/未测)显示占位符。
export const latencyText = (n: Node): string => (n.latency > 0 ? `${n.latency}ms` : '—')

// 标签展示:缺省(票据 21 前)返回空数组,由模板走空态,不报错。
// 返回中文标签(存储英文,展示中文化)。
export const tagsDisplay = (n: Node): string[] => (n.tags ?? []).map(tagLabel)

// 出网单元:国家码 + 泄露/代理警示。无出网信息返回 null(不占位)。
export interface EgressCell {
  code: string // 出口国家码(大写),未知为空
  warn: boolean // 是否有风险警示
  reasons: string[] // 警示原因(代理/机房/DNS 泄露)
}
export const examEgressCell = (report: ExamReport | undefined): EgressCell | null => {
  const ipv4 = report?.egress?.ipv4
  const dns = report?.egress?.dns
  const code = (ipv4?.country_code ?? '').toUpperCase()
  const reasons: string[] = []
  if (ipv4?.proxy) reasons.push('疑似代理/VPN 出口')
  if (ipv4?.hosting) reasons.push('机房 IP(非住宅)')
  if (dns?.leak) reasons.push('疑似 DNS 泄露')
  if (!code && reasons.length === 0) return null
  return { code, warn: reasons.length > 0, reasons }
}

// 节点行的体检派生视图:稳定性徽标 + 出网 + 体检相对时间,均由最近一次体检记录一次算出。
export interface NodeExamSummary {
  badge: ExamBadge | null // 稳定性分 + 色(无稳定性段为 null)
  egress: EgressCell | null // 出网国家码 + 警示
  relative: string // 体检时间(相对)
}
export const buildNodeExamSummary = (
  entry: ExamHistoryEntry | null,
  nowMs?: number
): NodeExamSummary | null => {
  if (!entry) return null
  return {
    badge: examBadgeSummary(entry, nowMs),
    egress: examEgressCell(entry.report),
    relative: relativeTimeZh(parseTimeMs(entry.created_at), nowMs)
  }
}

// 从当前节点集中收集可筛选的解锁能力目标(排除 connectivity/bandwidth 等通用探测),升序去重。
export const unlockTargetsOf = (nodes: Node[]): string[] => {
  const set = new Set<string>()
  for (const n of nodes) {
    if (!n.unlock_results) continue
    for (const t of Object.keys(n.unlock_results)) {
      if (!isGenericTarget(t)) set.add(t)
    }
  }
  return [...set].sort()
}

// 从当前节点集中收集全部标签(票据 21 前为空),升序去重。
export const tagsOf = (nodes: Node[]): string[] => {
  const set = new Set<string>()
  for (const n of nodes) {
    for (const t of n.tags ?? []) set.add(t)
  }
  return [...set].sort()
}

// 地区列显示:空/Unknown 显示"未知",其他原样返回。
export const regionDisplay = (region: string | undefined): string => {
  if (!region || region === 'Unknown') return '未知'
  return region
}
