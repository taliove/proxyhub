import type { SubscriptionConditions } from '@/types'

// parseConditions 把 endpoint.conditions 原始 JSON 串解析为规范化对象(缺字段补空)。
// 与 Go 侧 subfilter.Conditions 对齐;空串或非法 JSON 返回空条件(=全量)。
export function parseConditions(raw: string): SubscriptionConditions {
  const empty: SubscriptionConditions = { airports: [], regions: [], tags: [], keyword: '' }
  if (!raw) return empty
  try {
    const o = JSON.parse(raw) as Partial<SubscriptionConditions>
    return {
      airports: o.airports ?? [],
      regions: o.regions ?? [],
      tags: o.tags ?? [],
      keyword: o.keyword ?? ''
    }
  } catch {
    return empty
  }
}

// hasConditions 报告端点是否配置了非空节点范围(表格指示用)。
export function hasConditions(raw: string): boolean {
  const c = parseConditions(raw)
  return (
    c.airports.length > 0 || c.regions.length > 0 || c.tags.length > 0 || c.keyword.trim() !== ''
  )
}
