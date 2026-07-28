import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useLoginCaptcha } from './useLoginCaptcha'
import { issueCaptcha } from '@/api/auth'

vi.mock('@/api/auth', () => ({ issueCaptcha: vi.fn() }))
vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

const challenge = (id: string) => ({
  challenge_id: id,
  image_base64: `data:image/png;base64,${id}`
})

describe('useLoginCaptcha', () => {
  beforeEach(() => vi.clearAllMocks())

  it('初始休眠:不可见、不发签发请求、payload 为空', () => {
    const c = useLoginCaptcha()
    expect(c.visible.value).toBe(false)
    expect(c.payload()).toEqual({})
    expect(vi.mocked(issueCaptcha)).not.toHaveBeenCalled()
  })

  it('handleFailure(false) 保持休眠', async () => {
    const c = useLoginCaptcha()
    await c.handleFailure(false)
    expect(c.visible.value).toBe(false)
    expect(vi.mocked(issueCaptcha)).not.toHaveBeenCalled()
  })

  it('handleFailure(true) 首次激活并签发,payload 带 id 与答案', async () => {
    vi.mocked(issueCaptcha).mockResolvedValue(challenge('c1'))
    const c = useLoginCaptcha()
    await c.handleFailure(true)
    expect(c.visible.value).toBe(true)
    expect(c.imageSrc.value).toBe('data:image/png;base64,c1')
    c.answer.value = '1234'
    expect(c.payload()).toEqual({ captcha_id: 'c1', captcha_answer: '1234' })
  })

  it('再次失败换新 challenge 并清空答案', async () => {
    vi.mocked(issueCaptcha)
      .mockResolvedValueOnce(challenge('c1'))
      .mockResolvedValueOnce(challenge('c2'))
    const c = useLoginCaptcha()
    await c.handleFailure(true)
    c.answer.value = 'wrong'
    await c.handleFailure(true)
    expect(c.challengeId.value).toBe('c2')
    expect(c.answer.value).toBe('')
    expect(vi.mocked(issueCaptcha)).toHaveBeenCalledTimes(2)
  })

  it('签发失败清空图片但保留可见性,payload 不带残留 id', async () => {
    vi.mocked(issueCaptcha)
      .mockResolvedValueOnce(challenge('c1'))
      .mockRejectedValueOnce({
        response: { status: 429 }
      })
    const c = useLoginCaptcha()
    await c.handleFailure(true)
    await c.fetchChallenge()
    expect(c.visible.value).toBe(true)
    expect(c.imageSrc.value).toBe('')
    expect(c.payload()).toEqual({})
  })

  it('fetchChallenge 并发去重:进行中不重复签发', async () => {
    let resolve: (v: unknown) => void = () => {}
    vi.mocked(issueCaptcha).mockImplementation(
      () => new Promise((r) => (resolve = r as (v: unknown) => void))
    )
    const c = useLoginCaptcha()
    const first = c.fetchChallenge()
    await c.fetchChallenge()
    expect(vi.mocked(issueCaptcha)).toHaveBeenCalledTimes(1)
    resolve(challenge('c1'))
    await first
  })
})
