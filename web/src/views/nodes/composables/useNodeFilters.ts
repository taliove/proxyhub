import { reactive, watch } from 'vue'

export interface NodeFilters {
  region: string
  type: string
  available: string
  blocked: string
  stale: string
  source: string
}

// 筛选状态与查询参数组装(load 与"检测筛选结果"共用,避免重复)。
// 下拉类筛选 change 即生效(watch 触发 onChange);文本搜索(机场名)
// 按规范由回车/清空显式触发,见 NodeFilterBar 的 search 事件。
export function useNodeFilters(onChange: () => void) {
  const filters = reactive<NodeFilters>({
    region: '',
    type: '',
    available: '',
    blocked: '',
    stale: '',
    source: ''
  })

  const activeFilterParams = (): Record<string, string> => {
    const p: Record<string, string> = {}
    if (filters.region) p.region = filters.region
    if (filters.type) p.type = filters.type
    if (filters.available) p.available = filters.available
    if (filters.blocked) p.blocked = filters.blocked
    if (filters.stale) p.stale = filters.stale
    if (filters.source) p.source = filters.source
    return p
  }

  watch(
    () => [filters.region, filters.type, filters.available, filters.blocked, filters.stale],
    onChange
  )

  return { filters, activeFilterParams }
}
