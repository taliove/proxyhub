import { onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'

export interface DetectionScope {
  type: 'all' | 'query' | 'selected'
  query?: Record<string, string>
  node_keys?: string[]
}

// DetectionStatus 后端 /detection/status 响应(与 server.DetectionStatus 对齐)。
interface DetectionStatus {
  running: boolean
  total_nodes?: number
  completed_nodes?: number
}

// 解锁检测(检查动作 1「出网快速检测」):触发 + 2s 轮询 + 取消。
// 状态必须全页共享同一实例(页头告警条、批量栏按钮、清理弹窗都消费 running),
// 由装配层(index.vue)创建一次并分发。completed/total 供批量栏进度展示(x/N)。
export function useNodeDetection() {
  const running = ref(false)
  const completed = ref(0)
  const total = ref(0)
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
        const status = await client.get<unknown, DetectionStatus>('/detection/status')
        if (status.total_nodes !== undefined) total.value = status.total_nodes
        if (status.completed_nodes !== undefined) completed.value = status.completed_nodes
        if (!status.running) {
          running.value = false
          if (total.value > 0) completed.value = total.value
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
      // 触发即预置计数:selected 用给定 keys 数,all/query 待首个 status 回填。
      completed.value = 0
      total.value = scope.node_keys?.length ?? 0
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

  return { running, completed, total, trigger, cancel }
}
