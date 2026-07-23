import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { runSpeedtest, type SpeedtestOutcome } from './runner'

// runSpeedtest 现在调用后端代理测速 API(POST /api/speedtest/proxy-test),
// 不再直接 fetch /api/speedtest/download 等。测试用 fetch mock 验证:
// 1. 正确构造请求体(node_key、mode=full)
// 2. 正确解析后端返回的 snake_case 字段为 camelCase
// 3. HTTP 错误时抛出带后端 error 信息的 Error
// 4. AbortSignal 取消时透传

describe('runSpeedtest', () => {
  const mockResult = {
    down_mbps: 245.8,
    up_mbps: 89.3,
    idle_latency_ms: 28.5,
    jitter_ms: 3.2,
    elapsed_ms: 12500
  }

  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('should call backend proxy-test API with node_key and mode=full', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(mockResult), { status: 200 })
    )

    const callbacks = {
      onPhase: vi.fn(),
      onSample: vi.fn()
    }

    await runSpeedtest('1.2.3.4:443', callbacks)

    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/speedtest/proxy-test')
    expect(init?.method).toBe('POST')
    const body = JSON.parse(init?.body as string)
    expect(body.node_key).toBe('1.2.3.4:443')
    expect(body.mode).toBe('full')
  })

  it('should omit node_key when empty (direct mode)', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(mockResult), { status: 200 })
    )

    await runSpeedtest('')

    const body = JSON.parse(fetchMock.mock.calls[0][1]?.body as string)
    expect(body.node_key).toBeUndefined()
  })

  it('should convert snake_case response to camelCase outcome', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(mockResult), { status: 200 })
    )

    const outcome: SpeedtestOutcome = await runSpeedtest('node-key')

    expect(outcome.downMbps).toBe(245.8)
    expect(outcome.upMbps).toBe(89.3)
    expect(outcome.idleLatencyMs).toBe(28.5)
    expect(outcome.jitterMs).toBe(3.2)
  })

  it('should simulate phase transitions via callbacks', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(mockResult), { status: 200 })
    )

    const onPhase = vi.fn()
    await runSpeedtest('node-key', { onPhase })

    expect(onPhase).toHaveBeenCalledWith('latency')
    expect(onPhase).toHaveBeenCalledWith('download')
    expect(onPhase).toHaveBeenCalledWith('upload')
  })

  it('should throw Error with backend error message on HTTP error', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'node connection failed: timeout' }), {
        status: 500
      })
    )

    await expect(runSpeedtest('bad-node')).rejects.toThrow(
      'node connection failed: timeout'
    )
  })

  it('should fall back to HTTP status when error field missing', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response('not json', { status: 502 })
    )

    await expect(runSpeedtest('node-key')).rejects.toThrow('HTTP 502')
  })

  it('should pass AbortSignal to fetch', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(mockResult), { status: 200 })
    )

    const controller = new AbortController()
    await runSpeedtest('node-key', {}, controller.signal)

    expect(fetchMock.mock.calls[0][1]?.signal).toBe(controller.signal)
  })
})
