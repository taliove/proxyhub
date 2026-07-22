// 任务中心 API 封装:任务列表 / 详情 / 取消 + 晚间标签重算调度设置。
import client from './client'

// JobStatus 任务生命周期状态(与后端 jobs.Status 对齐)。
export type JobStatus = 'running' | 'done' | 'failed' | 'cancelled' | 'interrupted'

// Job 通用任务信息(后端 JobInfo)。cursor 为游标进度(已完成数的字符串)。
// params 为启动参数 JSON 串(含 node_keys/scope,用于生成可读范围标识)。
export interface Job {
  id: number
  kind: string
  key: string
  status: JobStatus
  cursor?: string
  params?: string
  created_at: string
  updated_at: string
}

// ScheduleConfig 晚间标签重算调度配置。
export interface ScheduleConfig {
  retag_time: string // "HH:MM" 零填充
  retag_enabled: boolean
}

// ListJobsFilter 任务列表可选过滤(对应后端 handleListJobs 的 kind/status 查询参数;
// status 支持逗号分隔多值,ANY 匹配,如 'failed,interrupted')。
export interface ListJobsFilter {
  kind?: string
  status?: string
}

// listJobs 拉取任务列表(后端按 created_at 倒序)。
// filter 缺省时请求 /jobs,行为与原先一致(向后兼容)。
export function listJobs(filter: ListJobsFilter = {}): Promise<Job[]> {
  const params = new URLSearchParams()
  if (filter.kind) params.set('kind', filter.kind)
  if (filter.status) params.set('status', filter.status)
  const qs = params.toString()
  return client.get<unknown, Job[]>(qs ? `/jobs?${qs}` : '/jobs')
}

// getJob 拉取单个任务详情。
export function getJob(id: number): Promise<Job> {
  return client.get<unknown, Job>(`/jobs/${id}`)
}

// cancelJob 取消运行中任务(按 kind + key 定位)。
export function cancelJob(kind: string, key: string): Promise<{ status: string }> {
  return client.post<unknown, { status: string }>(
    `/jobs/${encodeURIComponent(kind)}/${encodeURIComponent(key)}/cancel`
  )
}

// getSchedule 读取晚间标签重算调度配置。
export function getSchedule(): Promise<ScheduleConfig> {
  return client.get<unknown, ScheduleConfig>('/settings/schedule')
}

// saveSchedule 写入晚间标签重算调度配置。
export function saveSchedule(cfg: ScheduleConfig): Promise<{ status: string }> {
  return client.put<unknown, { status: string }>('/settings/schedule', cfg)
}
