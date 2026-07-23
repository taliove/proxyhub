// 本机实测的测量原语:全部走原生 fetch(下行 ReadableStream 流式读、上行流式写),
// 不经 axios(client.ts 30s timeout 且不可流式读)。axios 只用于结果落库/历史查询。
// 时长口径装进全局 30s 读写超时(main.go):下行默认 10s(服务端上限 25s),
// 上行由客户端到点停流,单次请求必在 30s 内结束。
import { computeLatencyMetrics, mbpsFromBytes, type LatencyMetrics } from './utils'

// PING_SAMPLES 延迟探测样本数:小请求串行打,算最小 RTT(空闲延迟)与相邻差(抖动)。
export const PING_SAMPLES = 8
// DOWNLOAD_DURATION_MS 下行时长(服务端 parseDownloadDuration 钳制到 [1s, 25s])。
export const DOWNLOAD_DURATION_MS = 10_000
// UPLOAD_DURATION_MS 上行时长:客户端到点停止发送即 EOF,服务端只计数。
export const UPLOAD_DURATION_MS = 10_000
// UPLOAD_CHUNK_SIZE 上行单次入队块(256KB,与下行发流块同尺寸)。
const UPLOAD_CHUNK_SIZE = 256 * 1024
// UPLOAD_FALLBACK_BYTES 不支持流式上传的浏览器(Firefox)的兜底:定时发一块定量数据。
const UPLOAD_FALLBACK_BYTES = 8 * 1024 * 1024

export type SpeedtestPhase = 'latency' | 'download' | 'upload'

// RunCallbacks 测速过程回调:阶段切换与实时速率(大数字实时刷新用)。
export interface RunCallbacks {
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

const pingUrl = (seq: number) => `/api/speedtest/ping?seq=${seq}`
const downloadUrl = (durationMs: number) => `/api/speedtest/download?duration_ms=${durationMs}`

// measureLatency 串行打 samples 次小请求,量 RTT 算空闲延迟/抖动。
export async function measureLatency(
  signal?: AbortSignal,
  samples = PING_SAMPLES
): Promise<LatencyMetrics> {
  const rtts: number[] = []
  for (let i = 0; i < samples; i++) {
    const t0 = performance.now()
    const res = await fetch(pingUrl(i), { cache: 'no-store', signal })
    await res.arrayBuffer()
    if (!res.ok) throw new Error(`ping failed: HTTP ${res.status}`)
    rtts.push(performance.now() - t0)
  }
  return computeLatencyMetrics(rtts)
}

// measureDownload 下行:fetch + ReadableStream 按到达节奏累计字节,到服务端停流(EOF)结束。
// onSample 每块回调一次实时速率;返回全程平均下行 Mbps。
export async function measureDownload(
  durationMs = DOWNLOAD_DURATION_MS,
  onSample?: (mbps: number) => void,
  signal?: AbortSignal
): Promise<number> {
  const res = await fetch(downloadUrl(durationMs), { cache: 'no-store', signal })
  if (!res.ok || !res.body) throw new Error(`download failed: HTTP ${res.status}`)
  const reader = res.body.getReader()
  const start = performance.now()
  let bytes = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    bytes += value.byteLength
    onSample?.(mbpsFromBytes(bytes, performance.now() - start))
  }
  return mbpsFromBytes(bytes, performance.now() - start)
}

// randomChunk 生成不可压缩随机块(crypto RNG;可压缩负载经中间层 gzip 会虚高)。
// 显式 ArrayBuffer 类型参数:TS 5.7+ 的 Uint8Array 泛型在 BlobPart 处要求非 Shared。
function randomChunk(size: number): Uint8Array<ArrayBuffer> {
  const chunk = new Uint8Array(new ArrayBuffer(size))
  crypto.getRandomValues(chunk)
  return chunk
}

// measureUpload 上行:流式 POST 随机块到点停流(duplex: 'half');服务端回 {bytes} 计数。
// 浏览器不支持请求流(Firefox)时退化定量 Blob 上传计时。
export async function measureUpload(
  durationMs = UPLOAD_DURATION_MS,
  onSample?: (mbps: number) => void,
  signal?: AbortSignal
): Promise<number> {
  const chunk = randomChunk(UPLOAD_CHUNK_SIZE)
  const start = performance.now()
  let sent = 0
  const body = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (performance.now() - start >= durationMs) {
        controller.close()
        return
      }
      controller.enqueue(chunk)
      sent += chunk.byteLength
      onSample?.(mbpsFromBytes(sent, performance.now() - start))
    }
  })
  try {
    // duplex 不在 TS dom 类型的 RequestInit 里,按 Chromium 扩展显式断言
    const init = { method: 'POST', body, duplex: 'half', cache: 'no-store', signal } as RequestInit
    const res = await fetch('/api/speedtest/upload', init)
    if (!res.ok) throw new Error(`upload failed: HTTP ${res.status}`)
    const data = (await res.json()) as { bytes?: number }
    const elapsed = performance.now() - start
    // 以服务端实收字节为准(客户端停流后服务端兜底截断口径一致)
    return mbpsFromBytes(typeof data.bytes === 'number' ? data.bytes : sent, elapsed)
  } catch (err) {
    if (signal?.aborted) throw err
    return measureUploadFallback(onSample, signal)
  }
}

// measureUploadFallback 无请求流支持的兜底:POST 定量随机 Blob,按耗时算上行速率。
async function measureUploadFallback(
  onSample?: (mbps: number) => void,
  signal?: AbortSignal
): Promise<number> {
  const blob = new Blob([randomChunk(UPLOAD_FALLBACK_BYTES)])
  const start = performance.now()
  const res = await fetch('/api/speedtest/upload', {
    method: 'POST',
    body: blob,
    cache: 'no-store',
    signal
  })
  if (!res.ok) throw new Error(`upload failed: HTTP ${res.status}`)
  await res.json()
  const mbps = mbpsFromBytes(UPLOAD_FALLBACK_BYTES, performance.now() - start)
  onSample?.(mbps)
  return mbps
}

// runSpeedtest 一键实测:延迟/抖动 → 下行 → 上行,阶段与实时速率经 callbacks 透出。
export async function runSpeedtest(
  callbacks: RunCallbacks = {},
  signal?: AbortSignal
): Promise<SpeedtestOutcome> {
  callbacks.onPhase?.('latency')
  const latency = await measureLatency(signal)
  callbacks.onPhase?.('download')
  const downMbps = await measureDownload(
    DOWNLOAD_DURATION_MS,
    (mbps) => callbacks.onSample?.('download', mbps),
    signal
  )
  callbacks.onPhase?.('upload')
  const upMbps = await measureUpload(
    UPLOAD_DURATION_MS,
    (mbps) => callbacks.onSample?.('upload', mbps),
    signal
  )
  return { downMbps, upMbps, idleLatencyMs: latency.idleLatencyMs, jitterMs: latency.jitterMs }
}
