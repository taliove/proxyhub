// 本机实测的测量原语:走后端 SSE 流式端点(GET /api/speedtest/proxy-test/stream),
// 后端经选中节点(或直连)访问测速端点,推送 latency/sample/done/error 帧。
// 浏览器用 EventSource 订阅,实时跳动数字(fast.com 式),对齐 BandwidthTestDialog 范式。
//
// 历史实现(浏览器直接 fetch /api/speedtest/download,测本机回环)已废弃(issue 0047)。

export type SpeedtestPhase = 'latency' | 'download' | 'upload'

// RunCallbacks 测速过程回调:onLatency(延迟抖动测完)、onSample(实时速率,每~300ms)、onPhase(阶段切换)。
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

// SSE 帧类型(与后端 handlers_speedtest_proxy.go 对齐)
interface LatencyFrame {
  phase: 'latency'
  idle_latency_ms: number
  jitter_ms: number
}
interface SampleFrame {
  phase: 'download' | 'upload'
  mbps: number
  elapsed_ms: number
}
interface DoneFrame {
  phase: 'done'
  down_mbps: number
  up_mbps: number
  idle_latency_ms: number
  jitter_ms: number
  elapsed_ms: number
}
interface ErrorFrame {
  phase: 'error'
  error: string
}

// runSpeedtest 一键实测:订阅后端 SSE 流,nodeKey 空串 = 直连基线。
// 实时帧经 callbacks 透出(onLatency/onSample/onPhase),done 帧作为最终结果 resolve。
// signal 觅 abort 时关闭 EventSource 并 reject。
export function runSpeedtest(
  nodeKey: string,
  callbacks: RunCallbacks = {},
  signal?: AbortSignal
): Promise<SpeedtestOutcome> {
  return new Promise<SpeedtestOutcome>((resolve, reject) => {
    const params = new URLSearchParams({ mode: 'full' })
    if (nodeKey) params.set('node_key', nodeKey)
    const url = `/api/speedtest/proxy-test/stream?${params.toString()}`
    const es = new EventSource(url, { withCredentials: true })

    let samplePhase: SpeedtestPhase | null = null

    const cleanup = () => {
      es.onmessage = null
      es.onerror = null
      es.close()
    }

    signal?.addEventListener('abort', () => {
      cleanup()
      reject(new Error('aborted'))
    })

    es.onmessage = (e) => {
      let frame: LatencyFrame | SampleFrame | DoneFrame | ErrorFrame
      try {
        frame = JSON.parse(e.data)
      } catch {
        return // 忽略非 JSON 帧
      }
      switch (frame.phase) {
        case 'latency':
          callbacks.onLatency?.(frame.idle_latency_ms, frame.jitter_ms)
          break
        case 'download':
        case 'upload':
          // 收到第一个 sample 帧即切换阶段(下行先,后上行)
          if (samplePhase !== frame.phase) {
            samplePhase = frame.phase
            callbacks.onPhase?.(frame.phase)
          }
          callbacks.onSample?.(frame.phase, frame.mbps)
          break
        case 'done':
          cleanup()
          resolve({
            downMbps: frame.down_mbps,
            upMbps: frame.up_mbps,
            idleLatencyMs: frame.idle_latency_ms,
            jitterMs: frame.jitter_ms
          })
          break
        case 'error':
          cleanup()
          reject(new Error(frame.error))
          break
      }
    }

    es.onerror = () => {
      // 连接错误(后端关闭/网络中断):若未 done,视为失败
      cleanup()
      reject(new Error('stream closed unexpectedly'))
    }
  })
}
