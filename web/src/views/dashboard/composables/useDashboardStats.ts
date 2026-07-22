import { onMounted, ref } from 'vue'
import client from '@/api/client'

// 主页统计数据契约,与后端 /dashboard/stats 响应对齐
export interface DashboardStats {
  totalNodes: number
  availableNodes: number
  endpoints: number
  airports: number
  lastUpdate: string
  avgLatency: number
}

const emptyStats = (): DashboardStats => ({
  totalNodes: 0,
  availableNodes: 0,
  endpoints: 0,
  airports: 0,
  lastUpdate: '-',
  avgLatency: 0
})

// useDashboardStats 拉取主页统计(/dashboard/stats),供 StatCards 模块消费。
// 失败时全局拦截器已提示,此处静默保留空态默认值。
export function useDashboardStats() {
  const stats = ref<DashboardStats>(emptyStats())

  onMounted(async () => {
    try {
      const data = await client.get<unknown, DashboardStats>('/dashboard/stats')
      stats.value = data
    } catch {
      // 全局拦截器已提示;保留空态
    }
  })

  return { stats }
}
