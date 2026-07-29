import { onMounted, ref } from 'vue'
import client from '@/api/client'
import { appBase } from '@/utils/base'
import type { Endpoint } from '@/types'

// useQuickEndpoints 拉取订阅地址列表(/endpoints),供主页 QuickEndpoints 模块消费。
// 失败时全局拦截器已提示,此处静默保留空列表;error 仅用于区分"空"与"加载失败"。
export function useQuickEndpoints() {
  const endpoints = ref<Endpoint[]>([])
  const loading = ref(true)
  const error = ref(false)

  onMounted(async () => {
    try {
      endpoints.value = await client.get<unknown, Endpoint[]>('/endpoints')
    } catch {
      // 全局拦截器已提示;保留空态并标记失败
      error.value = true
    } finally {
      loading.value = false
    }
  })

  // 拼装完整订阅 URL,与 Endpoints.vue 同一模式
  const getSubscriptionUrl = (row: Endpoint) =>
    `${window.location.origin}${appBase()}/sub/${row.path}?token=${row.token}`

  return { endpoints, loading, error, getSubscriptionUrl }
}
