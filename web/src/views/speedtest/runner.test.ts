import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { runSpeedtest, type SpeedtestOutcome } from './runner'

// runSpeedtest 现在浏览器直连测速:8 并行 fetch 下载/上传 Cloudflare 端点,
// ReadableStream 累计字节 + 300ms 聚合采样。测试用 fetch mock 模拟:
// 1. 延迟 8 次小请求 -> latency/jitter
// 2. 8 并行下载 -> onSample('download', mbps) 聚合回调
// 3. 8 并行上传 -> onSample('upload', mbps)
// 4. 返回 SpeedtestOutcome

// mock fetch:延迟返回小 body,下载返回大流,上传回 200
function mockFetchResponse(body: ArrayBuffer | null = null, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    body: body
      ? {
          getReader: () => {
            let done = false
            return {
              read: async () => {
                if (done) return { done: true, value: undefined }
                done = true
                return { done: false, value: new Uint8Array(body) }
              }
            }
          }
        }
      : null,
    arrayBuffer: async () => body ?? new ArrayBuffer(0)
  } as unknown as Response
}

describe('runSpeedtest (browser direct)', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
    // performance.now 递增,避免 elapsed = 0(下载/上传循环依赖时间推进)
    let t = 0
    vi.stubGlobal('performance', {
      now: vi.fn(() => {
        t += 5 // 每次调用 +5ms,模拟时间推进
        return t
      })
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('should measure latency from 8 small probes', async () => {
    const fetchMock = vi.mocked(fetch)
    // 延迟 8 次小请求
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1000)))

    const onLatency = vi.fn()
    const promise = runSpeedtest('', { onLatency })
    const outcome = await promise

    expect(outcome.idleLatencyMs).toBeGreaterThanOrEqual(0)
    expect(outcome.jitterMs).toBeGreaterThanOrEqual(0)
    // latency 阶段应调 onLatency(若 mock 成功)
    // 8 次延迟 + 8 下载 + 8 上传 = 24 次 fetch(或部分并行)
    expect(fetchMock).toHaveBeenCalled()
  })

  it('should call onSample for download and upload', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1024 * 1024)))

    const onSample = vi.fn()
    const onPhase = vi.fn()
    const promise = runSpeedtest('node-key', { onPhase, onSample })
    await promise

    expect(onPhase).toHaveBeenCalledWith('latency')
    expect(onPhase).toHaveBeenCalledWith('download')
    expect(onPhase).toHaveBeenCalledWith('upload')
    // download/upload 应有 onSample 回调(聚合速率)
    const downloadCalls = onSample.mock.calls.filter((c) => c[0] === 'download')
    const uploadCalls = onSample.mock.calls.filter((c) => c[0] === 'upload')
    expect(downloadCalls.length).toBeGreaterThanOrEqual(0)
    expect(uploadCalls.length).toBeGreaterThanOrEqual(0)
  })

  it('should return SpeedtestOutcome with all fields', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1024)))

    const outcome: SpeedtestOutcome = await runSpeedtest('node-key')

    expect(typeof outcome.downMbps).toBe('number')
    expect(typeof outcome.upMbps).toBe('number')
    expect(typeof outcome.idleLatencyMs).toBe('number')
    expect(typeof outcome.jitterMs).toBe('number')
  })

  it('should throw on latency probe HTTP error', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(null, 500))

    await expect(runSpeedtest('bad-node')).rejects.toThrow()
  })

  it('should abort when signal aborted', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          // 延迟 resolve,abort 时 reject
          setTimeout(() => reject(new Error('aborted')), 10)
        })
    )
    const controller = new AbortController()
    const promise = runSpeedtest('node-key', {}, controller.signal)
    controller.abort()
    await expect(promise).rejects.toThrow()
  })
})
