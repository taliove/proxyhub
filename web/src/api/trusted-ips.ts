// 受信 IP 管理 API 封装(ticket 10 后端 internal/server/handlers_trusted_ips.go)。
//
// 线上契约:
//   GET    /api/me/trusted-ips        -> {trusted, recommendations, auto_trust_ip, threshold}
//   POST   /api/me/trusted-ips {ip}   -> {ok, ip}         采纳推荐(未达阈值 400)
//   DELETE /api/me/trusted-ips/{ip}   -> {ok, ip}         撤销(幂等)
//   PUT    /api/me/trusted-ips/auto   -> {ok, auto_trust_ip}
//   POST   /api/admin/users/{id}/trusted-ips/clear -> {ok, removed}  超管专属
//
// 一律作用于"登录者本人",不随超管 impersonate 视角漂移(见后端注释)。
import client from './client'

// TrustedIP 一条受信 IP 授权。expired=true 的行仍会返回,便于用户看见并清理
// 已失效条目;地理字段在离线库无记录时为空字符串(私有/保留网段)。
export interface TrustedIP {
  ip: string
  expires_at: string
  last_used_at: string
  expired: boolean
  region_code: string
  region_name: string
}

// TrustRecommendation 一条信任推荐:窗口内 MFA 成功次数达阈值但当前未受信。
export interface TrustRecommendation {
  ip: string
  mfa_successes: number
  region_code: string
  region_name: string
}

// TrustedIPsEnvelope GET /api/me/trusted-ips 的响应信封。
export interface TrustedIPsEnvelope {
  trusted: TrustedIP[]
  recommendations: TrustRecommendation[]
  auto_trust_ip: boolean
  threshold: number
}

// listTrustedIPs 读取本人受信 IP 列表 + 推荐 + 自动信任开关状态。
export function listTrustedIPs(): Promise<TrustedIPsEnvelope> {
  return client.get<unknown, TrustedIPsEnvelope>('/me/trusted-ips')
}

// trustIP 采纳一条推荐(或不传 ip 时信任当前来源地址)。
// 未达阈值的陌生地址后端返回 400,由全局拦截器提示。
export function trustIP(ip?: string): Promise<{ ok: boolean; ip: string }> {
  return client.post<unknown, { ok: boolean; ip: string }>('/me/trusted-ips', ip ? { ip } : {})
}

// revokeTrustedIP 撤销一条受信 IP(该 IP 下次登录重新要求 MFA)。
export function revokeTrustedIP(ip: string): Promise<{ ok: boolean; ip: string }> {
  return client.delete<unknown, { ok: boolean; ip: string }>(
    `/me/trusted-ips/${encodeURIComponent(ip)}`
  )
}

// setAutoTrustIP 开关自动信任(达阈值自动入信任列表;默认关)。
export function setAutoTrustIP(enabled: boolean): Promise<{ ok: boolean; auto_trust_ip: boolean }> {
  return client.put<unknown, { ok: boolean; auto_trust_ip: boolean }>('/me/trusted-ips/auto', {
    enabled
  })
}

// clearUserTrustedIPs 超管清空目标用户全部受信 IP(逼回完整 MFA 挑战)。
export function clearUserTrustedIPs(
  userId: number
): Promise<{ ok: boolean; removed: number; username: string }> {
  return client.post<unknown, { ok: boolean; removed: number; username: string }>(
    `/admin/users/${userId}/trusted-ips/clear`,
    {}
  )
}
