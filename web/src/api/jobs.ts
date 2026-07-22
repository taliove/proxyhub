// 任务中心 API 封装:任务列表 / 详情 / 取消 / 任务结果 + 晚间标签重算调度设置。
import client from './client'
import type { ExamHistoryEntry } from '@/types'
import type { TestRun } from '@/composables/useAirportTest'

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

// JobResultReason 任务结果空原因(后端 handlers_job_result):
// no_report = 本次任务未产生报告(如被中断/窗口内无记录);
// kind_has_no_result = 该 kind 本无"报告"产物(batch_detection/retag_all 等)。
export type JobResultReason = 'no_report' | 'kind_has_no_result'

// ExamJobReport 任务结果里某节点的体检报告条目(后端 server.ExamJobReport)。
// fallback=true:报告由任务时间窗回退派生(任务结果关联前的旧数据),
// UI 据此标注"非本次任务产出"。
export interface ExamJobReport {
  node_key: string
  fallback: boolean
  entry?: ExamHistoryEntry
}

// JobResult GET /api/jobs/{id}/result 响应:exam/batch_exam 填 reports
// (exam 单份;batch_exam 按 params.node_keys 聚合,无报告的节点缺省不出现);
// airport_test 填 airport_test_run(airport_test_runs 按 job_id 反查,ADR 0027);
// 无报告时带 reason。
export interface JobResult {
  kind: string
  job_id: number
  reports: ExamJobReport[]
  airport_test_run?: TestRun | null
  reason?: JobResultReason
}

// getJobResult 拉取任务产出结果(按 kind 分发聚合,ticket 0022)。
export function getJobResult(id: number): Promise<JobResult> {
  return client.get<unknown, JobResult>(`/jobs/${id}/result`)
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
