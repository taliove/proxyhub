// 标签中文化映射:存储保持英文(derive.go 派生),展示层统一中文化。
// 单一事实源:所有标签消费处(节点表/筛选/订阅条件)均调用此函数。

// 两位国家码 -> 中文名映射表(常用国家/地区,按字母序)。
// 覆盖主流节点地区;未覆盖的返回原始码(向前兼容)。
const REGION_NAMES: Record<string, string> = {
  AU: '澳大利亚',
  BR: '巴西',
  CA: '加拿大',
  CN: '中国',
  DE: '德国',
  FR: '法国',
  HK: '香港',
  IN: '印度',
  JP: '日本',
  KR: '韩国',
  MO: '澳门',
  NL: '荷兰',
  RU: '俄罗斯',
  SG: '新加坡',
  TW: '台湾',
  UK: '英国',
  US: '美国'
}

// 固定标签 -> 中文映射表(对齐 internal/nodetag/derive.go 词表)。
const STATIC_LABELS: Record<string, string> = {
  // 解锁能力
  'nf-full': 'Netflix全解',
  'nf-originals': 'Netflix仅自制',
  'yt-premium': 'YouTube Premium',
  'disney-plus': 'Disney+',
  openai: 'OpenAI',
  claude: 'Claude',
  gemini: 'Gemini',
  // 稳定性档位
  'stable-good': '稳定·优',
  'stable-fair': '稳定·良',
  'stable-poor': '稳定·差',
  // 出网/质量
  fast: '高速',
  ipv6: 'IPv6',
  residential: '住宅',
  hosting: '机房',
  'dns-leak': 'DNS泄露'
}

/**
 * tagLabel 将英文标签转为中文展示文案。
 * - 固定标签:查 STATIC_LABELS 表
 * - region:XX 动态标签:提取国家码查 REGION_NAMES,未覆盖返回原始码
 * - 未知标签:原样返回(向前兼容)
 *
 * @param tag 英文标签(如 "nf-full" / "region:US" / "unknown-tag")
 * @returns 中文展示文案(如 "Netflix全解" / "美国" / "unknown-tag")
 */
export function tagLabel(tag: string): string {
  // 固定标签直接查表
  if (tag in STATIC_LABELS) {
    return STATIC_LABELS[tag]
  }

  // region:XX 动态标签:提取国家码转中文
  if (tag.startsWith('region:')) {
    const code = tag.slice('region:'.length).toUpperCase()
    return REGION_NAMES[code] || code // 未覆盖返回原始码
  }

  // 未知标签原样返回
  return tag
}
