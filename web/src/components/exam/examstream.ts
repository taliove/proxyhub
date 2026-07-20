// 体检 SSE 流控制器:封装 EventSource 生命周期、指数退避重连、序号去重与取消。
// 通过依赖注入(createEventSource/fetch/timer)让全部逻辑可用 mock 驱动单测,
// 组件只负责构造 URL、把回调接到响应式状态。
import type { ExamEvent } from '@/types'

// 重连预算:连续失败达到该次数才判定连接失败。
export const MAX_RECONNECT_ATTEMPTS = 5

// reconnectDelayMs 指数退避延迟(0 基次序):500ms 起翻倍,封顶 8000ms。
export function reconnectDelayMs(attempt: number): number {
  const base = 500 * 2 ** Math.max(0, attempt)
  return Math.min(base, 8000)
}

// shouldReconnect 是否还在重连预算内(attempt 为已用重连次数)。
export function shouldReconnect(attempt: number): boolean {
  return attempt < MAX_RECONNECT_ATTEMPTS
}

// isNewSeq 序号去重:仅接受严格递增的 seq;无 seq 的帧一律放行(容错)。
export function isNewSeq(lastSeq: number, seq: number | undefined): boolean {
  if (seq === undefined) return true
  return seq > lastSeq
}

// 流状态:连接中 / 直播中 / 重连中 / 完成 / 连接失败 / 已取消。
export type ExamStreamStatus =
  'connecting' | 'live' | 'reconnecting' | 'done' | 'error' | 'cancelled'

// EventSource 的最小接口(便于 mock;只用到这些成员)。
export interface EventSourceLike {
  onmessage: ((e: { data: string }) => void) | null
  onerror: ((e?: unknown) => void) | null
  onopen: ((e?: unknown) => void) | null
  close(): void
}

export interface ExamStreamDeps {
  createEventSource: (url: string) => EventSourceLike
  fetch: typeof fetch
  setTimer?: (cb: () => void, ms: number) => number
  clearTimer?: (handle: number) => void
}

export interface ExamStreamHandlers {
  // onFrame 仅投递去重后的新帧(非终态);终态经 onStatus 通知。
  onFrame: (frame: ExamEvent) => void
  onStatus: (status: ExamStreamStatus) => void
  // onTerminal 当终态由服务端帧触发时透传该帧(携带 error 文案 / report);
  // 重连预算耗尽的连接失败无帧,不触发。
  onTerminal?: (frame: ExamEvent) => void
}

const TERMINAL_PHASES = new Set<ExamEvent['phase']>(['done', 'error', 'cancelled'])

// ExamStream 单个体检流的控制器,一次 start 对应一次体检会话(可跨断线续传)。
export class ExamStream {
  private es: EventSourceLike | null = null
  private streamUrl = ''
  private lastSeq = 0
  private reconnects = 0
  private terminated = false
  private timerHandle: number | null = null
  private readonly setTimer: (cb: () => void, ms: number) => number
  private readonly clearTimer: (handle: number) => void

  constructor(
    private readonly deps: ExamStreamDeps,
    private readonly handlers: ExamStreamHandlers
  ) {
    this.setTimer = deps.setTimer ?? ((cb, ms) => setTimeout(cb, ms) as unknown as number)
    this.clearTimer = deps.clearTimer ?? ((h) => clearTimeout(h))
  }

  // start 打开流;URL 由组件构造(含 node 定位参数)。
  start(streamUrl: string): void {
    this.streamUrl = streamUrl
    this.lastSeq = 0
    this.reconnects = 0
    this.terminated = false
    this.setStatus('connecting')
    this.open()
  }

  // cancel POST 取消;不立即置终态,等待服务端回推 cancelled 帧以保留"收到后停止"语义。
  cancel(cancelUrl: string): Promise<void> {
    return this.deps
      .fetch(cancelUrl, { method: 'POST', credentials: 'include' })
      .then(() => undefined)
  }

  // dispose 关闭流并清理定时器(对话框关闭时调用)。
  dispose(): void {
    this.terminated = true
    this.clearPendingTimer()
    this.closeEs()
  }

  private open(): void {
    const es = this.deps.createEventSource(this.streamUrl)
    es.onopen = () => {
      // 成功握手即视为连接恢复,重置重连预算,回到直播态。
      this.reconnects = 0
      if (!this.terminated) this.setStatus('live')
    }
    es.onmessage = (e) => this.onMessage(e.data)
    es.onerror = () => this.onErrored()
    this.es = es
  }

  private onMessage(data: string): void {
    if (this.terminated) return
    let frame: ExamEvent
    try {
      frame = JSON.parse(data)
    } catch {
      // 半帧/非法负载:直播中忽略,交由后续帧继续。
      return
    }

    if (TERMINAL_PHASES.has(frame.phase)) {
      this.terminate(this.terminalStatus(frame.phase), frame)
      return
    }

    if (!isNewSeq(this.lastSeq, frame.seq)) return // 回放去重
    if (frame.seq !== undefined) this.lastSeq = frame.seq
    this.handlers.onFrame(frame)
  }

  private onErrored(): void {
    if (this.terminated) return
    this.closeEs()
    if (!shouldReconnect(this.reconnects)) {
      this.terminate('error')
      return
    }
    const delay = reconnectDelayMs(this.reconnects)
    this.reconnects += 1
    this.setStatus('reconnecting')
    this.timerHandle = this.setTimer(() => {
      this.timerHandle = null
      if (!this.terminated) this.open()
    }, delay)
  }

  private terminate(status: ExamStreamStatus, frame?: ExamEvent): void {
    this.terminated = true
    this.clearPendingTimer()
    this.closeEs()
    if (frame) this.handlers.onTerminal?.(frame)
    this.setStatus(status)
  }

  private terminalStatus(phase: ExamEvent['phase']): ExamStreamStatus {
    if (phase === 'cancelled') return 'cancelled'
    if (phase === 'error') return 'error'
    return 'done'
  }

  private setStatus(status: ExamStreamStatus): void {
    this.handlers.onStatus(status)
  }

  private closeEs(): void {
    this.es?.close()
    this.es = null
  }

  private clearPendingTimer(): void {
    if (this.timerHandle !== null) {
      this.clearTimer(this.timerHandle)
      this.timerHandle = null
    }
  }
}
