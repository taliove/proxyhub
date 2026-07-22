import { onMounted, ref } from 'vue'
import { listJobs, type Job } from '@/api/jobs'
import { isRunning } from '@/views/jobs/jobmeta'

// 非 running 任务保留条数(按 updated_at 倒序取最近)。
const RECENT_FINISHED_LIMIT = 5

// useActivityFeed 拉取任务活动流水:running 全部在前 + 非 running 最近 5 条。
// 本模块需要跨全部状态(running + 各终态),拉全量在前端过滤一次到位;
// 后端按 created_at 倒序返回,running 段保持该顺序,
// finished 段按 updated_at 重排。失败时全局拦截器已提示,此处静默降级为空列表。
export function useActivityFeed() {
  const jobs = ref<Job[]>([])
  const loading = ref(true)

  onMounted(async () => {
    try {
      const all = await listJobs()
      const running = all.filter((j) => isRunning(j.status))
      const finished = all
        .filter((j) => !isRunning(j.status))
        .slice()
        .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
        .slice(0, RECENT_FINISHED_LIMIT)
      jobs.value = [...running, ...finished]
    } catch {
      jobs.value = []
    } finally {
      loading.value = false
    }
  })

  return { jobs, loading }
}
