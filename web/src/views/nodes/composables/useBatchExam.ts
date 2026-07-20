import { onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import { listJobs } from '@/api/jobs'
import { parseCursor, isRunning } from '@/views/jobs/jobmeta'

// 批量体检:POST 启动后复用 jobs 轮询做轻量进度展示(完成 x/N,可取消)。
// 不接 SSE(详细采样在单节点体检弹窗,批量只需计数),与 useNodeDetection 同构。
export function useBatchExam(onDone?: () => void) {
  const running = ref(false)
  const completed = ref(0)
  const total = ref(0)
  let pollTimer: number | null = null

  const BATCH_EXAM_KIND = 'batch_exam'
  const BATCH_EXAM_KEY = 'batch_exam'
  const POLL_INTERVAL_MS = 3000

  const stopPolling = () => {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  const finish = () => {
    running.value = false
    stopPolling()
  }

  const poll = async () => {
    try {
      const jobs = await listJobs()
      const job = jobs.find((j) => j.kind === BATCH_EXAM_KIND && j.key === BATCH_EXAM_KEY)
      if (!job) return
      const done = parseCursor(job.cursor)
      if (done !== null) completed.value = done
      if (!isRunning(job.status)) {
        finish()
        completed.value = total.value
        ElMessage.success('批量体检完成')
        onDone?.()
      }
    } catch {
      finish()
    }
  }

  // start 启动批量体检。nodeKeys 为空 = 全节点(后端约定)。total 供进度展示。
  const start = async (nodeKeys: string[]) => {
    if (running.value) return
    try {
      await client.post('/nodes/exam/batch', { node_keys: nodeKeys })
      running.value = true
      completed.value = 0
      total.value = nodeKeys.length
      ElMessage.info(`批量体检已启动(${nodeKeys.length} 个节点)`)
      stopPolling()
      pollTimer = window.setInterval(poll, POLL_INTERVAL_MS)
    } catch (e) {
      running.value = false
      ElMessage.error(e instanceof Error ? e.message : '启动批量体检失败')
    }
  }

  const cancel = async () => {
    try {
      await client.post('/nodes/exam/batch/cancel', {})
      finish()
      ElMessage.info('已取消批量体检')
      onDone?.()
    } catch {
      finish()
    }
  }

  onUnmounted(stopPolling)

  return { running, completed, total, start, cancel }
}
