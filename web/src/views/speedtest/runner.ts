// 本机实测的测量原语:浏览器直连测速(fast.com 式)。
// 流量路径:浏览器 → 用户客户端代理(节点) → speed.cloudflare.com。
// 8 并行连接同时下载/上传,聚合带宽;每 ~300ms 聚合瞬时速率回调 onSample(实时跳动)。
// Cloudflare 端点响应头含 Access-Control-Allow-Origin: *,浏览器可直接 fetch(已验证)。
//
// 历史实现(后端代理测速 SSE)已废弃:流量不经浏览器,Network 看不到大 Size,
// 测的是 ProxyHub 服务器→节点,非用户真实链路。现改浏览器直连,Network 可见大 Size。

export type SpeedtestPhase = 'latency' | 'download' | 'upload'

// RunCallbacks 测速过程回调:onLatency(延迟抖动)、onPhase(阶段切换)、onSample(实时速率)。
export interface RunCallbacks {
  onLatency?: (idleLatencyMs: number, jitterMs: number) => void
  onPhase?: (phase: SpeedtestPhase) => void
  onSample?: (phase: SpeedtestPhase, mbps: number) => void
}

// SpeedtestOutcome 一次完整实测的产出(落库前的原始浮点,精度收敛在调用侧 round2)。
export interface SpeedtestOutcome {
  downMbps: number
  upMbps: number
  idleLatencyMs: number
  jitterMs: number
}

// 测速端点(ProxyHub 透传:浏览器 ← ProxyHub ← 节点 ← Cloudflare)。
// 浏览器 fetch 透传端点,后端经选中节点(node_key)访问 Cloudflare 并流式转发。
// 流量经浏览器(Network 可见大 Size),且经用户下拉选定节点(不依赖客户端代理选择)。
// 一律经 appBase() 拼接:Site Path 部署下裸 '/api' 会逃出前缀被反代 404。
import { appBase } from '@/utils/base'

const proxyDownloadURL = (nodeKey: string, i: number) =>
  `${appBase()}/api/speedtest/proxy-download/stream?${nodeKey ? `node_key=${encodeURIComponent(nodeKey)}&` : ''}i=${i}`
const proxyLatencyURL = (nodeKey: string, seq: number) =>
  `${appBase()}/api/speedtest/proxy-latency?${nodeKey ? `node_key=${encodeURIComponent(nodeKey)}&` : ''}seq=${seq}`
const proxyUploadURL = (nodeKey: string, i: number) =>
  `${appBase()}/api/speedtest/proxy-upload/stream?${nodeKey ? `node_key=${encodeURIComponent(nodeKey)}&` : ''}i=${i}`

// 默认参数
const PARALLEL_CONNECTIONS = 8
const DOWNLOAD_DURATION_MS = 10_000
const UPLOAD_DURATION_MS = 10_000
const LATENCY_SAMPLES = 8
const SAMPLE_INTERVAL_MS = 300 // 聚合采样间隔（对齐后端 detection.sampleReader）
const UPLOAD_CHUNK_SIZE = 256 * 1024 // 上行单次入队块（不可压缩随机数据）

// measureLatency 串行打 samples 次透传小请求(经节点),量 RTT 算空闲延迟/抖动。
async function measureLatency(
  nodeKey: string,
  signal: AbortSignal,
  onLatency?: (lat: number, jit: number) => void
): Promise<{ idleLatencyMs: number; jitterMs: number }> {
  const rtts: number[] = []
  for (let i = 0; i < LATENCY_SAMPLES; i++) {
    const t0 = performance.now()
    const res = await fetch(proxyLatencyURL(nodeKey, i), { cache: 'no-store', signal })
    await res.arrayBuffer()
    if (!res.ok) throw new Error(`latency probe ${i}: HTTP ${res.status}`)
    rtts.push(performance.now() - t0)
  }
  const { idleLatencyMs, jitterMs } = computeLatencyMetrics(rtts)
  onLatency?.(idleLatencyMs, jitterMs)
  return { idleLatencyMs, jitterMs }
}

// computeLatencyMetrics 从 RTT 样本算 latency(min)与 jitter(相邻差绝对值平均)。
function computeLatencyMetrics(rtts: number[]): { idleLatencyMs: number; jitterMs: number } {
  if (rtts.length === 0) return { idleLatencyMs: 0, jitterMs: 0 }
  const idleLatencyMs = Math.min(...rtts)
  if (rtts.length === 1) return { idleLatencyMs, jitterMs: 0 }
  let jitterSum = 0
  for (let i = 1; i < rtts.length; i++) {
    jitterSum += Math.abs(rtts[i] - rtts[i - 1])
  }
  return { idleLatencyMs, jitterMs: jitterSum / (rtts.length - 1) }
}

// measureDownload 8 并行连接同时 fetch 透传下载端点(经节点),ReadableStream 累计字节,
// 每 SAMPLE_INTERVAL_MS 聚合 8 连接总字节算瞬时速率回调 onSample。
// 到 deadline 或全 EOF 结束;聚合 Mbps = 总字节*8/耗时/1e6。
async function measureDownload(
  nodeKey: string,
  signal: AbortSignal,
  onSample?: (phase: SpeedtestPhase, mbps: number) => void
): Promise<number> {
  const start = performance.now()
  const deadline = start + DOWNLOAD_DURATION_MS
  let totalBytes = 0
  let winStart = start
  let winBytes = 0

  const sample = () => {
    const now = performance.now()
    const winElapsed = now - winStart
    if (winElapsed >= SAMPLE_INTERVAL_MS && onSample) {
      const mbps = (winBytes * 8) / (winElapsed / 1000) / 1e6
      onSample('download', mbps)
      winStart = now
      winBytes = 0
    }
  }

  const workers = Array.from({ length: PARALLEL_CONNECTIONS }, async (_, i) => {
    const res = await fetch(proxyDownloadURL(nodeKey, i), { cache: 'no-store', signal })
    if (!res.ok || !res.body) throw new Error(`download conn ${i}: HTTP ${res.status}`)
    const reader = res.body.getReader()
    for (;;) {
      if (performance.now() >= deadline) break
      const { done, value } = await reader.read()
      if (done) break
      totalBytes += value.byteLength
      winBytes += value.byteLength
      sample()
    }
  })

  await Promise.all(workers)
  const elapsed = performance.now() - start
  if (elapsed === 0) throw new Error('elapsed time is zero')
  // 以到 deadline 或全 EOF 时的总字节 / 实际耗时算聚合 Mbps
  return (totalBytes * 8) / (elapsed / 1000) / 1e6
}

// randomChunk 生成不可压缩随机块(可压缩负载经中间层 gzip 会虚高)。
// crypto.getRandomValues 单次上限 65536 字节,大块分批填充。
function randomChunk(size: number): Uint8Array<ArrayBuffer> {
  const chunk = new Uint8Array(new ArrayBuffer(size))
  const maxBatch = 65536
  for (let offset = 0; offset < size; offset += maxBatch) {
    const batchSize = Math.min(maxBatch, size - offset)
    crypto.getRandomValues(chunk.subarray(offset, offset + batchSize))
  }
  return chunk
}

// measureUpload 8 并行连接同时 POST 定量 Blob 到透传上传端点(经节点),
// 每连接循环发多个 1MB Blob 直到 deadline,累计字节算聚合瞬时速率。
// Chrome HTTP/1.1 拒发 duplex: 'half' streaming body(请求根本到不了后端),
// 故不用 streaming,改定量 Blob(浏览器自动带 Content-Length,后端 buffer 转发)。
async function measureUpload(
  nodeKey: string,
  signal: AbortSignal,
  onSample?: (phase: SpeedtestPhase, mbps: number) => void
): Promise<number> {
  const start = performance.now()
  const deadline = start + UPLOAD_DURATION_MS
  let totalBytes = 0
  let winStart = start
  let winBytes = 0

  const sample = () => {
    const now = performance.now()
    const winElapsed = now - winStart
    if (winElapsed >= SAMPLE_INTERVAL_MS && onSample) {
      const mbps = (winBytes * 8) / (winElapsed / 1000) / 1e6
      onSample('upload', mbps)
      winStart = now
      winBytes = 0
    }
  }

  const chunk = randomChunk(UPLOAD_CHUNK_SIZE) // 256KB 不可压缩随机块

  const workers = Array.from({ length: PARALLEL_CONNECTIONS }, async (_, i) => {
    // 循环 POST Blob 到 deadline(每次 POST 带 Content-Length,后端 buffer 转发)
    for (;;) {
      if (performance.now() >= deadline) break
      const blob = new Blob([chunk])
      const res = await fetch(proxyUploadURL(nodeKey, i), {
        method: 'POST',
        body: blob,
        cache: 'no-store',
        signal,
        headers: { 'Content-Type': 'application/octet-stream' }
      })
      if (!res.ok) throw new Error(`upload conn ${i}: HTTP ${res.status}`)
      await res.arrayBuffer().catch(() => {})
      totalBytes += chunk.byteLength
      winBytes += chunk.byteLength
      sample()
    }
  })

  await Promise.all(workers)
  const elapsed = performance.now() - start
  if (elapsed === 0) throw new Error('elapsed time is zero')
  return (totalBytes * 8) / (elapsed / 1000) / 1e6
}

// runSpeedtest 一键实测:延迟/抖动 → 8 并行下行 → 8 并行上行。
// 透传模式:浏览器 fetch ProxyHub 透传端点,后端经选定节点(nodeKey)访问 Cloudflare
// 并流式转发。流量经浏览器(Network 可见大 Size),且经用户下拉选定节点。
// 回调同现接口(onLatency/onPhase/onSample)。
export async function runSpeedtest(
  nodeKey: string, // 选定节点 key('' = 直连基线，后端直连 Cloudflare)
  callbacks: RunCallbacks = {},
  signal?: AbortSignal
): Promise<SpeedtestOutcome> {
  const controller = new AbortController()
  if (signal) {
    signal.addEventListener('abort', () => controller.abort(), { once: true })
  }
  const sig = controller.signal

  // 1. 延迟/抖动(经节点)
  callbacks.onPhase?.('latency')
  const { idleLatencyMs, jitterMs } = await measureLatency(nodeKey, sig, callbacks.onLatency)

  // 2. 8 并行下行(经节点透传)
  callbacks.onPhase?.('download')
  const downMbps = await measureDownload(nodeKey, sig, callbacks.onSample)

  // 3. 8 并行上行(经节点透传)
  callbacks.onPhase?.('upload')
  const upMbps = await measureUpload(nodeKey, sig, callbacks.onSample)

  return { downMbps, upMbps, idleLatencyMs, jitterMs }
}
