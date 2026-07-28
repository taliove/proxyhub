// 审计事件的展示口径:事件类型 -> 中文标签/标签色,以及 login_success 详情里的
// mfa 标记解析。单一事实源:Audit.vue 的过滤器选项、列表标签、详情徽标都取自这里。
//
// 后端口径(internal/server 的 recordAudit 调用):
//   - login_success / login_failure / honeypot_ban / threshold_ban 为既有事件
//   - captcha_failure(handlers_captcha.go)、mfa_enrolled / mfa_failure / mfa_reset
//     (handlers_mfa.go)为登录加固新增事件
//   - login_success 的 detail 携带二段标记:
//       mfa=totp / mfa=recovery      真实过了第二因子
//       mfa_skipped=trusted_ip       受信 IP 免验(后端刻意不写成 mfa=,
//                                    以免受信推荐引擎把"免验"算成一次二段成功)
//       空串                          未启用 MFA / 待补绑定的账号
//   GET /api/audit/events 不做 event_type 枚举校验(server/audit.go 直接把 CSV
//   拆开当 SQL 参数绑定),所以前端加类型无需后端同步改动。

export type EventTagType = 'success' | 'info' | 'warning' | 'danger'

interface EventMeta {
  label: string
  tag: EventTagType
}

// EVENT_META 的键即前端可过滤的事件类型全集;顺序决定过滤器下拉的顺序。
const EVENT_META: Record<string, EventMeta> = {
  login_success: { label: '登录成功', tag: 'success' },
  login_failure: { label: '登录失败', tag: 'warning' },
  captcha_failure: { label: '验证码失败', tag: 'warning' },
  mfa_failure: { label: 'MFA 失败', tag: 'warning' },
  mfa_enrolled: { label: 'MFA 绑定', tag: 'success' },
  // 重置会摘掉用户的第二因子,是安全等级下调的管理动作,按危险色提示。
  mfa_reset: { label: 'MFA 重置', tag: 'danger' },
  honeypot_ban: { label: '蜜罐封禁', tag: 'danger' },
  threshold_ban: { label: '阈值封禁', tag: 'danger' }
}

/** eventLabel 返回事件类型的中文展示名;未知类型原样返回(向前兼容后端新事件)。 */
export function eventLabel(eventType: string): string {
  return EVENT_META[eventType]?.label || eventType
}

/** eventTag 返回事件类型对应的 el-tag 类型;未知类型用中性色。 */
export function eventTag(eventType: string): EventTagType {
  return EVENT_META[eventType]?.tag || 'info'
}

/** EVENT_FILTER_OPTIONS 由 EVENT_META 派生,避免过滤器与展示映射两处各写一份。 */
export const EVENT_FILTER_OPTIONS: ReadonlyArray<{ label: string; value: string }> = Object.entries(
  EVENT_META
).map(([value, meta]) => ({ value, label: meta.label }))

export type MFAMarker = 'totp' | 'recovery' | 'trusted_ip'

export interface MFABadge {
  marker: MFAMarker
  label: string
  tag: EventTagType
}

const MFA_BADGES: Record<MFAMarker, { label: string; tag: EventTagType }> = {
  totp: { label: 'TOTP', tag: 'success' },
  recovery: { label: '恢复码', tag: 'warning' },
  trusted_ip: { label: '受信 IP 免验', tag: 'info' }
}

// detail 是空格/逗号分隔的 key=value 片段(如 "session=abc mfa=totp"),按整段
// token 匹配:子串匹配会把 mfa_skipped=trusted_ip 误判成 mfa= 标记。
const MARKER_BY_TOKEN: Record<string, MFAMarker> = {
  'mfa=totp': 'totp',
  'mfa=recovery': 'recovery',
  'mfa_skipped=trusted_ip': 'trusted_ip'
}

const detailTokens = (detail: string): string[] =>
  (detail || '')
    .split(/[\s,]+/)
    .map((t) => t.trim())
    .filter(Boolean)

/** mfaMarkerOf 从 detail 中解析二段标记;没有标记(未启用 MFA 等)返回 null。 */
export function mfaMarkerOf(detail: string): MFAMarker | null {
  for (const token of detailTokens(detail)) {
    const marker = MARKER_BY_TOKEN[token]
    if (marker) return marker
  }
  return null
}

/**
 * loginMFABadge 给出 login_success 行要展示的二段徽标,用来区分"过了 MFA 的登录"
 * 与"受信 IP 免验的登录"。非 login_success 事件或无标记返回 null。
 */
export function loginMFABadge(eventType: string, detail: string): MFABadge | null {
  if (eventType !== 'login_success') return null
  const marker = mfaMarkerOf(detail)
  if (!marker) return null
  return { marker, ...MFA_BADGES[marker] }
}

/**
 * detailText 返回详情列的文字部分:已被徽标表达的 mfa 标记从文本中摘掉,避免
 * "TOTP" 徽标旁边再重复一遍 "mfa=totp"。其余片段原样保留。
 */
export function detailText(eventType: string, detail: string): string {
  if (eventType !== 'login_success') return detail || ''
  return detailTokens(detail)
    .filter((token) => !MARKER_BY_TOKEN[token])
    .join(' ')
}
