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

// ProxyTestRequest 后端代理测速请求(与后端 speedtest.ProxyTestRequest 对齐)。
// node_key 与 self_node_id 二选一;都不传 = 直连基线。mode 默认 full。
export interface ProxyTestRequest {
  node_key?: string
  self_node_id?: number
  mode?: 'latency' | 'download' | 'upload' | 'full'
  download_duration_ms?: number
  upload_duration_ms?: number
}

// ProxyTestResult 后端代理测速结果(与后端 speedtest.ProxyTestResult 对齐)。
export interface ProxyTestResult {
  down_mbps: number
  up_mbps: number
  idle_latency_ms: number
  jitter_ms: number
  elapsed_ms: number
}

// runProxyTest 发起后端代理测速:浏览器发起,后端经选中节点(或直连)访问
// Cloudflare 测速端点,返回全链路带宽。用原生 fetch(不走 axios 拦截器),
// 以便正确支持 AbortSignal 取消与后端一次性返回。
export async function runProxyTest(
  payload: ProxyTestRequest,
  signal?: AbortSignal
): Promise<ProxyTestResult> {
  const res = await fetch('/api/speedtest/proxy-test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal
  })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const err = (await res.json()) as { error?: string }
      if (err.error) msg = err.error
    } catch {
      // 响应体非 JSON,保留 HTTP 状态码
    }
    throw new Error(msg)
  }
  return (await res.json()) as ProxyTestResult
}
