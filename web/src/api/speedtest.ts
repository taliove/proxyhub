// 本机实测(浏览器端 fast.com 式测速)API 封装:结果落库 / 历史查询 / 删除。
// 注意:测速流量本身(ping/download/upload)不走本模块——axios 30s timeout 且
// 不可流式读,下行/上行必须用原生 fetch + ReadableStream(见 views/speedtest/runner.ts)。
import client from './client'

// SpeedtestResult 一条本机实测历史(与后端 store.SpeedtestResult 对齐)。
// node_key 空串 = 直连/未标注;标注节点已不在池的孤儿历史照常读出(标"已失效")。
export interface SpeedtestResult {
  id: number
  node_key: string
  down_mbps: number
  up_mbps: number
  idle_latency_ms: number
  jitter_ms: number
  client_info: string
  created_at: string // RFC3339
}

// SaveSpeedtestPayload 落库一条实测结果的请求体(数值必须有限且非负,后端校验)。
export interface SaveSpeedtestPayload {
  node_key: string // '' = 直连/未标注
  down_mbps: number
  up_mbps: number
  idle_latency_ms: number
  jitter_ms: number
  client_info: string
}

// saveSpeedtestResult 落库一条实测结果,返回新行 id。
export function saveSpeedtestResult(payload: SaveSpeedtestPayload): Promise<{ id: number }> {
  return client.post<unknown, { id: number }>('/speedtest/results', payload)
}

// listSpeedtestResults 拉取实测历史(后端按时间倒序;全量,不过滤)。
export function listSpeedtestResults(): Promise<SpeedtestResult[]> {
  return client.get<unknown, SpeedtestResult[]>('/speedtest/results')
}

// deleteSpeedtestResult 按 id 删除一条实测历史。
export function deleteSpeedtestResult(id: number): Promise<{ deleted: boolean }> {
  return client.delete<unknown, { deleted: boolean }>(`/speedtest/results/${id}`)
}
// 注:本机实测的测速原语走 SSE 流(runner.ts 的 runSpeedtest,EventSource 订阅
// /api/speedtest/proxy-test/stream),不在此处。本模块仅负责结果落库/历史查询/删除。
