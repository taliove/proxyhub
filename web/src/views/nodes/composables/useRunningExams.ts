import { ref, onMounted, onUnmounted } from 'vue'
import { listJobs } from '@/api/jobs'
import type { Job } from '@/api/jobs'

// useRunningExams 轮询任务中心,提取进行中的 exam 任务 key 集合(node_key)。
// 用于在节点行显示"查看进度"而非"深度体检"按钮。
// 轮询间隔 10s,页面可见时执行。
export function useRunningExams(intervalMs = 10000) {
  const runningExamKeys = ref<Set<string>>(new Set())
  let timer: ReturnType<typeof setInterval> | null = null

  const load = async () => {
    try {
      const jobs = await listJobs()
      const examKeys = jobs
        .filter((job: Job) => job.kind === 'exam' && job.status === 'running')
        .map((job: Job) => job.key)
      runningExamKeys.value = new Set(examKeys)
    } catch (err) {
      console.error('Failed to load running exams:', err)
    }
  }

  const startPolling = () => {
    load() // 立即加载一次
    timer = setInterval(load, intervalMs) // 周期轮询（默认 10s,测试可注入更短间隔）
  }

  const stopPolling = () => {
    if (timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }

  onMounted(() => {
    startPolling()
  })

  onUnmounted(() => {
    stopPolling()
  })

  return {
    runningExamKeys,
    reload: load
  }
}
