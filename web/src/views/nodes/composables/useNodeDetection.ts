import { onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'

export interface DetectionScope {
  type: 'all' | 'query' | 'selected'
  query?: Record<string, string>
  node_keys?: string[]
}

// 解锁检测:触发 + 2s 轮询 + 取消。
// 状态必须全页共享同一实例(页头告警条、批量栏按钮、清理弹窗都消费 running),
// 由装配层(index.vue)创建一次并分发。
export function useNodeDetection() {
  const running = ref(false)
  let pollTimer: number | null = null

  const stopPolling = () => {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  const startPolling = (onComplete?: () => void) => {
    stopPolling()
    pollTimer = window.setInterval(async () => {
      try {
        const status = await client.get<unknown, { running: boolean }>('/detection/status')
        if (!status.running) {
          running.value = false
          stopPolling()
          ElMessage.success('检测完成')
          onComplete?.()
        }
      } catch {
        stopPolling()
        running.value = false
      }
    }, 2000)
  }

  const trigger = async (scope: DetectionScope, onComplete?: () => void, startMessage?: string) => {
    try {
      await client.post('/detection/trigger', scope)
      running.value = true
      ElMessage.info(startMessage || '检测已启动')
      startPolling(onComplete)
    } catch (e) {
      ElMessage.error(e instanceof Error ? e.message : '启动检测失败')
    }
  }

  const cancel = async (onComplete?: () => void) => {
    try {
      await client.post('/detection/cancel', {})
      running.value = false
      stopPolling()
      ElMessage.info('已取消检测')
      onComplete?.()
    } catch (e) {
      ElMessage.error(e instanceof Error ? e.message : '取消失败')
    }
  }

  onUnmounted(stopPolling)

  return { running, trigger, cancel }
}
