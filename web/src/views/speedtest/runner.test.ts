import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { runSpeedtest, type SpeedtestOutcome } from './runner'

// runSpeedtest 现在订阅后端 SSE 流(EventSource),消费 latency/sample/done/error 帧。
// 测试用 EventSource mock 模拟后端推送,验证:
// 1. 正确构造 URL(带 node_key, mode=full)
// 2. latency 帧 -> onLatency 回调
// 3. sample 帧 -> onSample 回调(实时跳动)
// 4. done 帧 -> resolve SpeedtestOutcome(snake_case -> camelCase)
// 5. error 帧 -> reject
// 6. signal abort -> close + reject

// MockEventSource:可手动触发 onmessage/onerror
class MockEventSource {
  url: string
  onmessage: ((e: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(url: string) {
    this.url = url
  }
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
  close() {
    this.closed = true
  }
}

let mockES: MockEventSource

describe('runSpeedtest', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'EventSource',
      vi.fn((url: string) => {
        mockES = new MockEventSource(url)
        return mockES
      })
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('should construct stream URL with node_key and mode=full', async () => {
    const ES = vi.mocked(EventSource)
    const promise = runSpeedtest('1.2.3.4:443')
    mockES.emit({
      phase: 'done',
      down_mbps: 1,
      up_mbps: 2,
      idle_latency_ms: 0,
      jitter_ms: 0,
      elapsed_ms: 0
    })
    await promise

    expect(ES).toHaveBeenCalledOnce()
    expect(mockES.url).toContain('/api/speedtest/proxy-test/stream')
    expect(mockES.url).toContain('node_key=1.2.3.4%3A443')
    expect(mockES.url).toContain('mode=full')
  })

  it('should omit node_key param when empty (direct mode)', async () => {
    const promise = runSpeedtest('')
    mockES.emit({
      phase: 'done',
      down_mbps: 1,
      up_mbps: 1,
      idle_latency_ms: 1,
      jitter_ms: 1,
      elapsed_ms: 1
    })
    await promise
    expect(mockES.url).not.toContain('node_key=')
  })

  it('should call onLatency on latency frame', async () => {
    const onLatency = vi.fn()
    const promise = runSpeedtest('n', { onLatency })
    mockES.emit({ phase: 'latency', idle_latency_ms: 42, jitter_ms: 3.2 })
    mockES.emit({
      phase: 'done',
      down_mbps: 0,
      up_mbps: 0,
      idle_latency_ms: 42,
      jitter_ms: 3.2,
      elapsed_ms: 0
    })
    await promise

    expect(onLatency).toHaveBeenCalledWith(42, 3.2)
  })

  it('should call onPhase + onSample on download sample frame', async () => {
    const onPhase = vi.fn()
    const onSample = vi.fn()
    const promise = runSpeedtest('n', { onPhase, onSample })
    mockES.emit({ phase: 'download', mbps: 150.5, elapsed_ms: 300 })
    mockES.emit({ phase: 'download', mbps: 160, elapsed_ms: 600 })
    mockES.emit({
      phase: 'done',
      down_mbps: 155,
      up_mbps: 0,
      idle_latency_ms: 0,
      jitter_ms: 0,
      elapsed_ms: 600
    })
    await promise

    // 第一个 download 帧切阶段 + 采样;第二个只采样(不重复切阶段)
    expect(onPhase).toHaveBeenCalledWith('download')
    expect(onPhase).toHaveBeenCalledTimes(1)
    expect(onSample).toHaveBeenCalledWith('download', 150.5)
    expect(onSample).toHaveBeenCalledWith('download', 160)
  })

  it('should resolve SpeedtestOutcome on done frame', async () => {
    const promise = runSpeedtest('n')
    mockES.emit({
      phase: 'done',
      down_mbps: 245.8,
      up_mbps: 89.3,
      idle_latency_ms: 28.5,
      jitter_ms: 3.2,
      elapsed_ms: 12500
    })
    const outcome: SpeedtestOutcome = await promise

    expect(outcome.downMbps).toBe(245.8)
    expect(outcome.upMbps).toBe(89.3)
    expect(outcome.idleLatencyMs).toBe(28.5)
    expect(outcome.jitterMs).toBe(3.2)
    expect(mockES.closed).toBe(true)
  })

  it('should reject on error frame', async () => {
    const promise = runSpeedtest('bad-node')
    mockES.emit({ phase: 'error', error: 'node connection failed' })
    await expect(promise).rejects.toThrow('node connection failed')
    expect(mockES.closed).toBe(true)
  })

  it('should reject and close on stream error', async () => {
    const promise = runSpeedtest('n')
    mockES.onerror?.()
    await expect(promise).rejects.toThrow('stream closed unexpectedly')
    expect(mockES.closed).toBe(true)
  })

  it('should abort on signal', async () => {
    const controller = new AbortController()
    const promise = runSpeedtest('n', {}, controller.signal)
    controller.abort()
    await expect(promise).rejects.toThrow('aborted')
    expect(mockES.closed).toBe(true)
  })
})
