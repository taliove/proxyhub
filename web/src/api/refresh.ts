// 刷新记录 API(refresh_runs 结果详情表;任务中心按 job_id 关联展示)
import client from './client'
import type { RefreshRun, RefreshEvent, RefreshFetchDiag } from '@/types'

// listRefreshRuns 拉取最近刷新记录(服务端保留最近 50 条)。
export function listRefreshRuns(): Promise<RefreshRun[]> {
  return client.get<unknown, RefreshRun[]>('/refresh/runs')
}

// getRefreshRun 拉取单条刷新记录、事件流与每机场拉取诊断。
export function getRefreshRun(
  id: number
): Promise<{ run: RefreshRun; events: RefreshEvent[]; diags: RefreshFetchDiag[] }> {
  return client.get<
    unknown,
    { run: RefreshRun; events: RefreshEvent[]; diags: RefreshFetchDiag[] }
  >(`/refresh/runs/${id}`)
}

// findRefreshRunByJob 按关联任务 id 反查刷新记录;任务刚启动记录可能未创建,调用方负责重试。
export async function findRefreshRunByJob(jobId: number): Promise<RefreshRun | null> {
  const runs = await listRefreshRuns()
  return runs.find((r) => r.job_id === jobId) ?? null
}
