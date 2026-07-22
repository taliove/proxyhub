import { computed, onMounted, ref } from 'vue'
import client from '@/api/client'
import type { Endpoint } from '@/types'

// 拉取统计全局汇总契约,与后端 /stats/global 响应对齐(自原 Stats.vue 迁入)
export interface GlobalPullStats {
  total_pulls: number
  unique_ips: number
  active_endpoints: number
}

// 拉取趋势点契约,与后端 /stats/trend 响应对齐
export interface TrendPoint {
  date: string
  endpoint_id: number
  alias: string
  count: number
}

const emptyGlobal = (): GlobalPullStats => ({
  total_pulls: 0,
  unique_ips: 0,
  active_endpoints: 0
})

// usePullStats 拉取订阅地址拉取统计(/stats/global、/stats/trend、/endpoints),
// 供 PullStats 模块消费。失败时全局拦截器已提示,此处静默保留空态默认值。
export function usePullStats() {
  const global = ref<GlobalPullStats>(emptyGlobal())
  const trend = ref<TrendPoint[]>([])
  const trendDays = ref(7)
  const endpoints = ref<Endpoint[]>([])
  const selectedEndpoint = ref<number | null>(null)

  const hasTrend = computed(() => trend.value.length > 0)

  const loadGlobal = async () => {
    try {
      global.value = await client.get<unknown, GlobalPullStats>('/stats/global')
    } catch {
      // 全局拦截器已提示;保留空态
    }
  }

  const loadTrend = async () => {
    try {
      const data = await client.get<unknown, { trend: TrendPoint[] }>(
        `/stats/trend?days=${trendDays.value}`
      )
      trend.value = data.trend || []
    } catch {
      trend.value = []
    }
  }

  const loadEndpoints = async () => {
    try {
      endpoints.value = await client.get<unknown, Endpoint[]>('/endpoints')
      if (endpoints.value.length && !selectedEndpoint.value) {
        selectedEndpoint.value = endpoints.value[0].id
      }
    } catch {
      // 全局拦截器已提示;保留空态
    }
  }

  onMounted(() => {
    loadGlobal()
    loadTrend()
    loadEndpoints()
  })

  return { global, trend, trendDays, endpoints, selectedEndpoint, hasTrend, loadTrend }
}
