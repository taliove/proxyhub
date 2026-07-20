// 深度体检历史查询 API 封装(实时体检走 SSE,见 NodeExamDialog)。
import client from './client'
import type { ExamHistoryEntry } from '@/types'

// ExamNodeRef 体检节点定位:机场节点用 node_key,自建节点用 self_node_id。
export interface ExamNodeRef {
  node_key?: string
  self_node_id?: number
}

function refParams(ref: ExamNodeRef): Record<string, string> {
  const params: Record<string, string> = {}
  if (ref.node_key) params.node_key = ref.node_key
  if (ref.self_node_id !== undefined) params.self_node_id = String(ref.self_node_id)
  return params
}

// fetchExamLatest 查询某节点最近一次体检;无历史后端返回 null。
export function fetchExamLatest(ref: ExamNodeRef): Promise<ExamHistoryEntry | null> {
  return client.get<unknown, ExamHistoryEntry | null>('/nodes/exam/latest', {
    params: refParams(ref)
  })
}

// fetchExamHistory 查询某节点体检历史(后端按时间倒序,每节点最多 50 条);无历史返回 []。
export function fetchExamHistory(ref: ExamNodeRef): Promise<ExamHistoryEntry[]> {
  return client.get<unknown, ExamHistoryEntry[]>('/nodes/exam/history', { params: refParams(ref) })
}
