import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { runSpeedtest, type SpeedtestOutcome } from './runner'

// runSpeedtest 现在浏览器 fetch ProxyHub 透传端点(/api/speedtest/proxy-download/stream 等),
// 后端经选定节点(nodeKey)访问 Cloudflare 并流式转发。8 并行 fetch 聚合带宽。
// 测试用 fetch mock 模拟透传端点响应:
// 1. 延迟 8 次透传小请求 -> latency/jitter
// 2. 8 并行透传下载 -> onSample('download', mbps)
// 3. 8 并行透传上传 -> onSample('upload', mbps)
// 4. 返回 SpeedtestOutcome
// 5. URL 带 node_key(经选定节点)

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

describe('runSpeedtest (passthrough)', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
    let t = 0
    vi.stubGlobal('performance', {
      now: vi.fn(() => {
        t += 5
        return t
      })
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('should construct passthrough URL with node_key', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1000)))

    await runSpeedtest('node.example.com:443')

    // 检查 fetch 调用 URL 带透传端点 + node_key
    const urls = fetchMock.mock.calls.map((c) => c[0] as string)
    expect(urls.some((u) => u.includes('/api/speedtest/proxy-latency'))).toBe(true)
    expect(urls.some((u) => u.includes('node_key='))).toBe(true)
  })

  it('should prefix passthrough URLs with the site path (window.__PH_BASE__)', async () => {
    // Site Path 部署:裸 '/api' 会被反代 404,所有透传 URL 必须带部署前缀。
    window.__PH_BASE__ = '/sp-test'
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1000)))
    try {
      await runSpeedtest('node.example.com:443')
      const urls = fetchMock.mock.calls.map((c) => c[0] as string)
      expect(urls.length).toBeGreaterThan(0)
      for (const u of urls) {
        expect(u.startsWith('/sp-test/api/speedtest/')).toBe(true)
      }
    } finally {
      delete window.__PH_BASE__
    }
  })

  it('should measure latency from 8 passthrough probes', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1000)))

    const onLatency = vi.fn()
    const outcome = await runSpeedtest('node-key', { onLatency })

    expect(outcome.idleLatencyMs).toBeGreaterThanOrEqual(0)
    expect(outcome.jitterMs).toBeGreaterThanOrEqual(0)
    expect(onLatency).toHaveBeenCalled()
  })

  it('should call onSample for download and upload', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1024 * 1024)))

    const onSample = vi.fn()
    const onPhase = vi.fn()
    await runSpeedtest('node-key', { onPhase, onSample })

    expect(onPhase).toHaveBeenCalledWith('latency')
    expect(onPhase).toHaveBeenCalledWith('download')
    expect(onPhase).toHaveBeenCalledWith('upload')
    // download/upload 应有透传端点 fetch
    const urls = fetchMock.mock.calls.map((c) => c[0] as string)
    expect(urls.some((u) => u.includes('/api/speedtest/proxy-download/stream'))).toBe(true)
    expect(urls.some((u) => u.includes('/api/speedtest/proxy-upload/stream'))).toBe(true)
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
          setTimeout(() => reject(new Error('aborted')), 10)
        })
    )
    const controller = new AbortController()
    const promise = runSpeedtest('node-key', {}, controller.signal)
    controller.abort()
    await expect(promise).rejects.toThrow()
  })

  it('should omit node_key when empty (direct baseline)', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(mockFetchResponse(new ArrayBuffer(1000)))

    await runSpeedtest('')

    const urls = fetchMock.mock.calls.map((c) => c[0] as string)
    // 直连基线:URL 不含 node_key
    expect(urls.every((u) => !u.includes('node_key='))).toBe(true)
    expect(urls.some((u) => u.includes('/api/speedtest/proxy-latency'))).toBe(true)
  })
})
