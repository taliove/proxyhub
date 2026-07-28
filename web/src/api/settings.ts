// 系统设置 API 封装(多租户,见 CONTEXT.md「租户级设置」)。
// 后端 GET /api/settings 返回信封:{settings: 生效值, overridden: 每键是否有用户覆盖};
// 写入走 {settings: {...}, reset: [...]} 信封(reset = 删除覆盖,回到跟随全局默认)。
import client from './client'
import type { DirectEgressSettings } from '@/types'

// 直连出口设置默认值(与后端 fail-open 默认对齐;留空键读取时由后端补默认值)。
export const DIRECT_EGRESS_DEFAULTS: DirectEgressSettings = {
  direct_egress_enabled: 'true',
  direct_egress_doh_url: '',
  direct_egress_interface: ''
}

// 租户级设置键:普通用户可读写(落 user_settings,读取回退全局默认);
// 超管未 impersonate 时编辑的是全局默认。与后端 tenantSettingKeys 保持一致。
export const TENANT_SETTINGS_KEYS = [
  'scheduled_refresh_enabled',
  'filter_keywords',
  'filter_whitelist',
  'standardize_names',
  'name_template'
] as const

// 超管专属设置键:只出现在超管的全局视图,普通用户提交会被后端忽略。
export const ADMIN_SETTINGS_KEYS = [
  'ban_threshold',
  'ban_duration',
  // 验证码触发次数(见 internal/server/security.go loadSecurityPolicy):
  // 0 = 每次登录都要验证码,N = 该 IP 失败 N 次后才要。后端允许 0,所以是
  // parseNonNegativeInt 而非正整数。
  'captcha_trigger_threshold',
  'feishu_webhook',
  'min_available_nodes',
  'fetch_concurrency',
  'bandwidth_down_url',
  'bandwidth_up_url',
  'bandwidth_up_bytes',
  'bandwidth_test_duration_sec',
  'bandwidth_timeout_sec',
  'bandwidth_dir_timeout_sec',
  'bandwidth_min_down_mbps',
  'bandwidth_min_up_mbps',
  // 拉取防护参数(pull-guard tickets 04/05/09):
  'pull_rate_limit_per_hour',
  'pull_blacklist_escalation_count',
  'pull_blacklist_duration'
] as const

// 设置页主表单自己的 settings 键白名单(超管全局视图 = 租户级 + 超管专属):
// 挂载时读取的是全量键(含 direct_egress_* 等其他 tab 的键),全量回写会把挂载时的
// 旧值静默覆盖其他 tab 刚保存的新值;各 tab 只写自己的键,互不覆盖。
export const MAIN_SETTINGS_KEYS = [...TENANT_SETTINGS_KEYS, ...ADMIN_SETTINGS_KEYS] as const

// SettingsEnvelope GET /api/settings 的信封响应。
export interface SettingsEnvelope {
  settings: Record<string, string>
  overridden: Record<string, boolean>
}

// getSettings 读取设置(视角驱动:超管=全局视图,普通用户=租户级生效视图+覆盖标记)。
export function getSettings(): Promise<SettingsEnvelope> {
  return client.get<unknown, SettingsEnvelope>('/settings')
}

// saveSettings 写入设置(视角驱动:超管=写全局默认,普通用户=写本人覆盖)。
// reset 列出的键删除覆盖(普通用户回到跟随全局默认)。
export function saveSettings(
  payload: Record<string, string>,
  reset: string[] = []
): Promise<{ status: string }> {
  return client.post<unknown, { status: string }>('/settings', { settings: payload, reset })
}
