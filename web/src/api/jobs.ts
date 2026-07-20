// 任务中心 API 封装:任务列表 / 详情 / 取消 + 晚间标签重算调度设置。
import client from './client'

// JobStatus 任务生命周期状态(与后端 jobs.Status 对齐)。
export type JobStatus = 'running' | 'done' | 'failed' | 'cancelled' | 'interrupted'

// Job 通用任务信息(后端 JobInfo)。cursor 为游标进度(已完成数的字符串)。
export interface Job {
  id: number
  kind: string
  key: string
  status: JobStatus
  cursor?: string
  created_at: string
  updated_at: string
}

// ScheduleConfig 晚间标签重算调度配置。
export interface ScheduleConfig {
  retag_time: string // "HH:MM" 零填充
  retag_enabled: boolean
}

// listJobs 拉取任务列表(后端按 created_at 倒序)。
export function listJobs(): Promise<Job[]> {
  return client.get<unknown, Job[]>('/jobs')
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
