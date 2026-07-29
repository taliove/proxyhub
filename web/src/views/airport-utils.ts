import type { Airport } from '@/types'

/**
 * Extract airport subscription URL for QR code display
 * @param airport Airport object containing subscription URL
 * @returns Airport subscription URL
 */
export function getAirportQRContent(airport: Airport): string {
  return airport.url
}

// ---------- 用量信息展示辅助(CONTEXT.md「用量信息」;spec-manual-airport-import) ----------

export const GIB = 1024 ** 3

/** 临期阈值(天):距过期不足此值标红 */
export const EXPIRING_SOON_DAYS = 7
/** 流量将尽阈值(剩余百分比):低于此值标红 */
export const USAGE_LOW_PERCENT = 10

/** formatBytes 字节数人性化(1 位小数;0 也格式化,调用方负责空态判断)。 */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** usageRemaining 剩余流量(字节);无总量信息返回 null(未知,不展示)。 */
export function usageRemaining(a: Airport): number | null {
  const total = a.usage_total ?? 0
  if (total <= 0) return null
  const used = (a.usage_upload ?? 0) + (a.usage_download ?? 0)
  return Math.max(0, total - used)
}

/** usageRemainingPercent 剩余百分比(0-100);未知返回 null。 */
export function usageRemainingPercent(a: Airport): number | null {
  const remaining = usageRemaining(a)
  if (remaining === null) return null
  return Math.round((remaining / (a.usage_total ?? 1)) * 100)
}

/** isUsageLow 流量将尽(剩余百分比低于阈值);未知不算将尽。 */
export function isUsageLow(a: Airport): boolean {
  const pct = usageRemainingPercent(a)
  return pct !== null && pct < USAGE_LOW_PERCENT
}

/** expireAt 过期时间(Date);未知返回 null。 */
export function expireAt(a: Airport): Date | null {
  const exp = a.usage_expire ?? 0
  if (exp <= 0) return null
  return new Date(exp * 1000)
}

/** expireText 过期日期展示(YYYY-MM-DD);未知返回 null。 */
export function expireText(a: Airport): string | null {
  const d = expireAt(a)
  if (!d) return null
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

/** isExpiringSoon 临期(已过期或距过期不足阈值天数);未知不算临期。 */
export function isExpiringSoon(a: Airport, now: Date = new Date()): boolean {
  const d = expireAt(a)
  if (!d) return false
  return d.getTime() - now.getTime() < EXPIRING_SOON_DAYS * 24 * 3600 * 1000
}

/** isExpired 已过期;未知不算。 */
export function isExpired(a: Airport, now: Date = new Date()): boolean {
  const d = expireAt(a)
  return d !== null && d.getTime() <= now.getTime()
}

/** gbToBytes GB 输入转字节(用量手填字段;null/负数归 0)。 */
export function gbToBytes(gb: number | null | undefined): number {
  if (gb === null || gb === undefined || !Number.isFinite(gb) || gb <= 0) return 0
  return Math.round(gb * GIB)
}

/** bytesToGb 字节转 GB 展示值(两位小数;0 返回 null 表示未填)。 */
export function bytesToGb(bytes: number | null | undefined): number | null {
  if (!bytes || bytes <= 0) return null
  return Math.round((bytes / GIB) * 100) / 100
}

/** airportUsageForm 从机场行初始化用量表单(编辑/重新粘贴预填用)。 */
export interface UsageFormValue {
  remainingGb: number | null
  totalGb: number | null
  expireDate: string // YYYY-MM-DD;'' = 未填
  webPageUrl: string
}

export function usageFormFromAirport(a: Airport): UsageFormValue {
  const remaining = usageRemaining(a)
  return {
    remainingGb: remaining === null ? null : bytesToGb(remaining),
    totalGb: bytesToGb(a.usage_total),
    expireDate: expireText(a) ?? '',
    webPageUrl: a.web_page_url ?? ''
  }
}

/** usageFormToPayload 用量表单转接口载荷(字节/unix 秒;全空返回 null 表示不提供)。 */
export function usageFormToPayload(v: UsageFormValue): {
  usage_remaining: number
  usage_total: number
  usage_expire: number
  web_page_url: string
} | null {
  const empty =
    (v.remainingGb === null || v.remainingGb <= 0) &&
    (v.totalGb === null || v.totalGb <= 0) &&
    !v.expireDate &&
    !v.webPageUrl.trim()
  if (empty) return null
  const expireMs = v.expireDate ? new Date(`${v.expireDate}T23:59:59`).getTime() : 0
  return {
    usage_remaining: gbToBytes(v.remainingGb),
    usage_total: gbToBytes(v.totalGb),
    usage_expire: expireMs > 0 ? Math.floor(expireMs / 1000) : 0,
    web_page_url: v.webPageUrl.trim()
  }
}

/**
 * usageFormToPayloadOrZero 编辑/重新粘贴语义:全空也发零值载荷——
 * 显式清空必须到达后端(SetAirportUsageForUser 空值 = 清空),省略字段会变成"不动"。
 */
export function usageFormToPayloadOrZero(v: UsageFormValue): {
  usage_remaining: number
  usage_total: number
  usage_expire: number
  web_page_url: string
} {
  return (
    usageFormToPayload(v) ?? {
      usage_remaining: 0,
      usage_total: 0,
      usage_expire: 0,
      web_page_url: ''
    }
  )
}
