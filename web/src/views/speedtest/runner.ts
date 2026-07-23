// 本机实测的测量原语:全部走后端代理测速 API(POST /api/speedtest/proxy-test),
// 后端经选中节点(或直连)访问 Cloudflare 测速端点,测全链路带宽。
//
// 历史实现(浏览器直接 fetch /api/speedtest/download 等)测的是本机回环,
// 数值虚高且与节点无关,已废弃(issue 0047)。保留下方常量供历史引用与默认时长。
//
// 时长口径装进全局 30s 读写超时(main.go):下行默认 10s,上行默认 10s,
// 加延迟探测 ~1s,单次总耗时约 21-25s。

// DOWNLOAD_DURATION_MS 下行测速时长(后端钳制)。
export const DOWNLOAD_DURATION_MS = 10_000
// UPLOAD_DURATION_MS 上行测速时长(后端钳制)。
export const UPLOAD_DURATION_MS = 10_000

export type SpeedtestPhase = 'latency' | 'download' | 'upload'

// RunCallbacks 测速过程回调:阶段切换(前端模拟,后端一次性返回)。
// onSample 不再使用(后端无实时速率推送),保留接口兼容调用方。
export interface RunCallbacks {
  onPhase?: (phase: SpeedtestPhase) => void
  onSample?: (phase: SpeedtestPhase, mbps: number) => void
}

// SpeedtestOutcome 一次完整实测的产出(落库前的原始浮点,精度收敛在调用侧 round2)。
// 与后端 ProxyTestResult 字段一致,但用驼峰命名对齐历史调用方。
export interface SpeedtestOutcome {
  downMbps: number
  upMbps: number
  idleLatencyMs: number
  jitterMs: number
}

// runSpeedtest 一键实测:调用后端代理测速 API。
// nodeKey 空串 = 直连基线;非空 = 经该节点代理测速。
// 阶段切换经 callbacks 透出(前端模拟 latency→download→upload,因后端一次性返回)。
export async function runSpeedtest(
  nodeKey: string,
  callbacks: RunCallbacks = {},
  signal?: AbortSignal
): Promise<SpeedtestOutcome> {
  // 前端模拟阶段切换(后端无流式进度,一次性返回结果)
  callbacks.onPhase?.('latency')
  callbacks.onPhase?.('download')
  callbacks.onPhase?.('upload')

  const result = await runProxyTestApi(
    {
      node_key: nodeKey || undefined,
      mode: 'full',
      download_duration_ms: DOWNLOAD_DURATION_MS,
      upload_duration_ms: UPLOAD_DURATION_MS
    },
    signal
  )

  return {
    downMbps: result.down_mbps,
    upMbps: result.up_mbps,
    idleLatencyMs: result.idle_latency_ms,
    jitterMs: result.jitter_ms
  }
}

// runProxyTestApi 延迟导入避免循环依赖:runner 被 useSpeedtestRun 引用,
// useSpeedtestRun 不应间接引用 api/speedtest 的 axios client。
// 直接 inline fetch 逻辑,与 api/speedtest.ts 的 runProxyTest 等价。
async function runProxyTestApi(
  payload: {
    node_key?: string
    self_node_id?: number
    mode?: string
    download_duration_ms?: number
    upload_duration_ms?: number
  },
  signal?: AbortSignal
): Promise<{
  down_mbps: number
  up_mbps: number
  idle_latency_ms: number
  jitter_ms: number
  elapsed_ms: number
}> {
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
      // 响应体非 JSON
    }
    throw new Error(msg)
  }
  return (await res.json()) as {
    down_mbps: number
    up_mbps: number
    idle_latency_ms: number
    jitter_ms: number
    elapsed_ms: number
  }
}
