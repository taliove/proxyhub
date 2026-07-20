import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import type { NodeTestResult } from '@/types'

// 即时测试档位。带宽测试(bandwidth)交由 BandwidthTestDialog 组件处理(需过程态 UI),
// 此 composable 只负责 quick/real 两档的消息提示式测试。
export type TestMode = 'quick' | 'real'

const MODE_LABELS: Record<TestMode, string> = {
  quick: '快测',
  real: '真实检测'
}

export function useNodeTest() {
  const testing = ref(false)

  const testNode = async (
    payload: { self_node_id?: number; node_key?: string },
    mode: TestMode
  ): Promise<NodeTestResult | null> => {
    if (testing.value) return null
    testing.value = true

    const modeLabel = MODE_LABELS[mode]
    const loading = ElMessage({ message: `测试中(${modeLabel})…`, duration: 0 })

    try {
      const res = await client.post<any, NodeTestResult>('/nodes/test', { ...payload, mode })
      loading.close()

      if (res.available) {
        ElMessage.success(`可用 · 延迟 ${res.latency}ms`)
      } else {
        ElMessage.error(`不可用:${res.error || '未知原因'}`)
      }
      return res
    } catch (e: any) {
      loading.close()
      ElMessage.error(e?.response?.data || '测试失败')
      return null
    } finally {
      testing.value = false
    }
  }

  return { testing, testNode }
}
