import { onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import { listJobs } from '@/api/jobs'
import { parseCursor, isRunning } from '@/views/jobs/jobmeta'

// useBatchJob 批量任务的通用轮询驱动:POST 启动后复用 jobs 轮询做轻量进度(完成 x/N,可取消)。
// 不接 SSE(详细采样在单节点弹窗,批量只需计数)。批量检查动作(深度体检 / 出网+稳定性 /
// 快速测速)三者结构同构,共用本 composable,只在 kind/端点/文案上差异化。
export interface BatchJobOptions {
  kind: string // jobs 表 kind(用于轮询匹配,如 batch_exam）
  key: string // jobs 表 key(全局单例任务固定 key)
  startUrl: string // 启动端点(POST /nodes/.../batch)
  cancelUrl: string // 取消端点(POST /nodes/.../batch/cancel)
  actionLabel: string // 动作中文名(用于启动/完成/失败提示)
}

const POLL_INTERVAL_MS = 3000

export function useBatchJob(opts: BatchJobOptions, onDone?: () => void) {
  const running = ref(false)
  const completed = ref(0)
  const total = ref(0)
  let pollTimer: number | null = null

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
      const job = jobs.find((j) => j.kind === opts.kind && j.key === opts.key)
      if (!job) return
      const done = parseCursor(job.cursor)
      if (done !== null) completed.value = done
      if (!isRunning(job.status)) {
        finish()
        completed.value = total.value
        ElMessage.success(`${opts.actionLabel}完成`)
        onDone?.()
      }
    } catch {
      finish()
    }
  }

  // start 启动批量任务。extraBody 供动作差异化参数(如深度体检 mode=full)。
  const start = async (nodeKeys: string[], extraBody: Record<string, unknown> = {}) => {
    if (running.value) return
    try {
      await client.post(opts.startUrl, { node_keys: nodeKeys, ...extraBody })
      running.value = true
      completed.value = 0
      total.value = nodeKeys.length
      ElMessage.info(`${opts.actionLabel}已启动(${nodeKeys.length} 个节点)`)
      stopPolling()
      pollTimer = window.setInterval(poll, POLL_INTERVAL_MS)
    } catch (e) {
      running.value = false
      ElMessage.error(e instanceof Error ? e.message : `启动${opts.actionLabel}失败`)
    }
  }

  const cancel = async () => {
    try {
      await client.post(opts.cancelUrl, {})
      finish()
      ElMessage.info(`已取消${opts.actionLabel}`)
      onDone?.()
    } catch {
      finish()
    }
  }

  onUnmounted(stopPolling)

  return { running, completed, total, start, cancel }
}
