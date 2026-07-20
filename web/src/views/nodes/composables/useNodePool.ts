import { ref } from 'vue'
import type { Node, NodePage } from '@/types'
import client from '@/api/client'

// 地区选项的接口形态(后端返回大写键,与 types 里 RegionOption 的小写定义不一致,
// 维持既有运行时行为,类型问题另行处理)
export interface RegionItem {
  Code: string
  Name: string
}

// 统一节点表在客户端做过滤/排序/分页(谓词见 predicates.ts),因此一次性取全量池。
// 池规模有界(本地管理面,机场节点数量级为百),客户端处理开销可忽略。
const POOL_PAGE_SIZE = 100000

// useNodePool 拉取全量节点池 + 参考数据(地区、机场来源)。
// 不再承担筛选/分页(下沉到 useNodeQuery + predicates),保持单一职责。
export function useNodePool() {
  const nodes = ref<Node[]>([])
  const regions = ref<RegionItem[]>([])
  const airportSources = ref<string[]>([])
  const loading = ref(false)
  const lastUpdate = ref('')

  const load = async () => {
    loading.value = true
    try {
      const data = await client.get<unknown, NodePage>('/nodes', {
        params: { page: 1, page_size: POOL_PAGE_SIZE }
      })
      nodes.value = data.nodes || []
      lastUpdate.value = data.last_update || ''
    } finally {
      loading.value = false
    }
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
    load,
    loadRegions,
    loadAirportSources
  }
}
