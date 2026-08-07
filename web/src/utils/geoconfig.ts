// 地域白名单配置口径(pull-guard ticket 08):geo_mode 三档展示与国家代码映射。
// 后端契约(internal/store/endpoint_geo.go):geo_countries/geo_provinces 均为逗号分隔字符串;
// 前端展示层用数组,存储层转换在此文件。

export type GeoModeTagType = 'info' | 'warning' | 'danger'

interface GeoModeMeta {
  label: string
  tag: GeoModeTagType
  desc: string
}

// GEO_MODE_META 三档口径:off 默认不判,observe 先观察,enforce 强制拦截
const GEO_MODE_META: Record<string, GeoModeMeta> = {
  off: { label: '关闭', tag: 'info', desc: '不判定地域，所有位置可拉取' },
  observe: {
    label: '观察',
    tag: 'warning',
    desc: '判定但不拦截，留痕记录「地域观察」状态用于确认规则准确性'
  },
  enforce: { label: '拦截', tag: 'danger', desc: '不匹配白名单则拒绝拉取，记录「地域拦截」' }
}

/** geoModeLabel 返回 geo_mode 的中文展示名;空值归 off(后端默认)。 */
export function geoModeLabel(mode: string): string {
  return GEO_MODE_META[mode || 'off']?.label || mode
}

/** geoModeTag 返回 geo_mode 对应的 el-tag 类型。 */
export function geoModeTag(mode: string): GeoModeTagType {
  return GEO_MODE_META[mode || 'off']?.tag || 'info'
}

/** geoModeDesc 返回 geo_mode 的说明文案。 */
export function geoModeDesc(mode: string): string {
  return GEO_MODE_META[mode || 'off']?.desc || ''
}

// COUNTRY_OPTIONS 常见国家代码与中文名(ISO 3166-1 alpha-2);
// 后端 NormalizeGeoCountries 会大写存储,前端展示时保持大写。
export const COUNTRY_OPTIONS: ReadonlyArray<{ code: string; name: string }> = [
  { code: 'CN', name: '中国' },
  { code: 'HK', name: '香港' },
  { code: 'TW', name: '台湾' },
  { code: 'MO', name: '澳门' },
  { code: 'SG', name: '新加坡' },
  { code: 'JP', name: '日本' },
  { code: 'KR', name: '韩国' },
  { code: 'US', name: '美国' },
  { code: 'GB', name: '英国' },
  { code: 'DE', name: '德国' },
  { code: 'FR', name: '法国' },
  { code: 'CA', name: '加拿大' },
  { code: 'AU', name: '澳大利亚' },
  { code: 'RU', name: '俄罗斯' },
  { code: 'IN', name: '印度' },
  { code: 'BR', name: '巴西' },
  { code: 'NL', name: '荷兰' },
  { code: 'SE', name: '瑞典' },
  { code: 'CH', name: '瑞士' },
  { code: 'IT', name: '意大利' },
  { code: 'ES', name: '西班牙' },
  { code: 'TR', name: '土耳其' },
  { code: 'TH', name: '泰国' },
  { code: 'MY', name: '马来西亚' },
  { code: 'PH', name: '菲律宾' },
  { code: 'VN', name: '越南' },
  { code: 'ID', name: '印度尼西亚' },
  { code: 'AR', name: '阿根廷' },
  { code: 'MX', name: '墨西哥' },
  { code: 'ZA', name: '南非' }
]

/**
 * parseGeoList 解析后端逗号分隔字符串为数组;空串返回空数组。
 * 与后端 store.ParseGeoList 对齐:去空白、去空条目。
 */
export function parseGeoList(raw: string): string[] {
  if (!raw) return []
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
}

/**
 * joinGeoList 将数组序列化为后端逗号分隔字符串;空数组返回空串。
 * 去重与大小写归一化留给后端 NormalizeGeo* 函数。
 */
export function joinGeoList(arr: string[]): string {
  return arr.filter((s) => s.trim() !== '').join(',')
}
