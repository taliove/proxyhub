// 系统设置 API 封装:/api/settings 为 map[string]string 透传契约(ticket 0019 产物)。
import client from './client'
import type { DirectEgressSettings } from '@/types'

// 直连出口设置默认值(与后端 fail-open 默认对齐;留空键读取时由后端补默认值)。
export const DIRECT_EGRESS_DEFAULTS: DirectEgressSettings = {
  direct_egress_enabled: 'true',
  direct_egress_doh_url: '',
  direct_egress_interface: ''
}

// 设置页主表单自己的 settings 键白名单:主保存 payload 只含这些键。
// 挂载时读取的是全量键(含 direct_egress_* 等其他 tab 的键),全量回写会把挂载时的
// 旧值静默覆盖其他 tab 刚保存的新值;各 tab 只写自己的键,互不覆盖。
export const MAIN_SETTINGS_KEYS = [
  'ban_threshold',
  'ban_duration',
  'feishu_webhook',
  'min_available_nodes',
  'scheduled_refresh_enabled',
  'fetch_concurrency',
  'filter_keywords',
  'filter_whitelist',
  'standardize_names',
  'name_template',
  'bandwidth_down_url',
  'bandwidth_up_url',
  'bandwidth_up_bytes',
  'bandwidth_test_duration_sec',
  'bandwidth_timeout_sec',
  'bandwidth_dir_timeout_sec',
  'bandwidth_min_down_mbps',
  'bandwidth_min_up_mbps'
] as const

// getSettings 读取全部设置键(map[string]string 透传;未设置的键缺省)。
export function getSettings(): Promise<Record<string, string>> {
  return client.get<unknown, Record<string, string>>('/settings')
}

// saveSettings 写入设置键(后端合并语义;值统一序列化为字符串,数字/布尔会导致 400)。
export function saveSettings(payload: Record<string, string>): Promise<{ status: string }> {
  return client.post<unknown, { status: string }>('/settings', payload)
}
