import { reactive, ref } from 'vue'
import type { Node, NodePage } from '@/types'
import client from '@/api/client'

// 地区选项的接口形态(后端返回大写键,与 types 里 RegionOption 的小写定义不一致,
// 维持既有运行时行为,类型问题另行处理)
export interface RegionItem {
  Code: string
  Name: string
}

// 节点列表数据获取:分页/排序/参考数据(地区、机场来源)。
// 筛选参数由调用方注入(activeFilterParams),避免与 useNodeFilters 互相依赖。
export function useNodeList(activeFilterParams: () => Record<string, string>) {
  const nodes = ref<Node[]>([])
  const regions = ref<RegionItem[]>([])
  const airportSources = ref<string[]>([])
  const loading = ref(false)
  const lastUpdate = ref('')
  const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
  const sort = reactive({ by: 'latency', order: 'asc' })

  const load = async () => {
    loading.value = true
    try {
      const params: Record<string, string | number> = {
        page: pagination.page,
        page_size: pagination.pageSize,
        sort_by: sort.by,
        sort_order: sort.order,
        ...activeFilterParams()
      }
      const data = await client.get<unknown, NodePage>('/nodes', { params })
      nodes.value = data.nodes || []
      pagination.total = data.total || 0
      lastUpdate.value = data.last_update || ''
    } finally {
      loading.value = false
    }
  }

  const onSortChange = ({ prop, order }: { prop: string; order: string | null }) => {
    if (!order) {
      sort.by = 'latency'
      sort.order = 'asc'
    } else {
      sort.by = prop
      sort.order = order === 'ascending' ? 'asc' : 'desc'
    }
    pagination.page = 1
    load()
  }

  const loadRegions = async () => {
    const data = await client.get<unknown, { regions: RegionItem[] }>('/settings/regions')
    regions.value = data.regions || []
  }

  const loadAirportSources = async () => {
    const data = await client.get<unknown, { name: string }[]>('/airports')
    airportSources.value = (data || []).map((a) => a.name).sort()
  }

  return {
    nodes,
    regions,
    airportSources,
    loading,
    lastUpdate,
    pagination,
    load,
    onSortChange,
    loadRegions,
    loadAirportSources
  }
}
