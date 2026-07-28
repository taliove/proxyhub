// Tests for the axios response interceptor's error-toast rules:
// surface {message} or {error} bodies, stay silent on 409 and on
// skipErrorToast, fall back to a generic Chinese message otherwise.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage } from 'element-plus'
import axios from 'axios'

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

type RejectionHandler = (error: unknown) => Promise<never>

// Self-contained mock: the captured rejection handler lives inside the
// factory closure (top-level vars would be in TDZ when the client module
// registers its interceptor during import).
vi.mock('axios', () => {
  const captured: { onErr?: RejectionHandler } = {}
  return {
    default: {
      __captured: captured,
      create: () => ({
        interceptors: {
          response: {
            use: (_onOk: unknown, onErr: RejectionHandler) => {
              captured.onErr = onErr
            }
          }
        }
      })
    }
  }
})

// Importing the client registers the interceptor on the mocked instance.
import '@/api/client'

const captured = (axios as unknown as { __captured: { onErr?: RejectionHandler } }).__captured

const makeError = (status: number, data: unknown, config: Record<string, unknown> = {}) => ({
  response: { status, data },
  config
})

describe('api client interceptor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('surfaces the server message field', async () => {
    await expect(
      captured.onErr!(makeError(400, { message: 'old password is incorrect' }))
    ).rejects.toBeTruthy()
    expect(ElMessage.error).toHaveBeenCalledWith('old password is incorrect')
  })

  it('falls back to the error field when message is absent', async () => {
    await expect(
      captured.onErr!(makeError(400, { error: 'username is reserved' }))
    ).rejects.toBeTruthy()
    expect(ElMessage.error).toHaveBeenCalledWith('username is reserved')
  })

  it('uses the generic Chinese message when the body carries neither field', async () => {
    await expect(captured.onErr!(makeError(500, {}))).rejects.toBeTruthy()
    expect(ElMessage.error).toHaveBeenCalledWith('请求失败')
  })

  it('stays silent on 409 (callers handle conflicts)', async () => {
    await expect(
      captured.onErr!(makeError(409, { error: 'username already taken' }))
    ).rejects.toBeTruthy()
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('stays silent when the request sets skipErrorToast', async () => {
    await expect(
      captured.onErr!(makeError(400, { error: 'username is reserved' }, { skipErrorToast: true }))
    ).rejects.toBeTruthy()
    expect(ElMessage.error).not.toHaveBeenCalled()
  })
})

// 403 + must_enroll_mfa 跳绑定页(ticket 08),与 must_change_password 同构。
// window.location 在 jsdom 里不可直接赋值,这里换成可写的桩对象。
describe('403 must_enroll_mfa redirect', () => {
  let loc: { pathname: string; href: string }

  beforeEach(() => {
    vi.clearAllMocks()
    loc = { pathname: '/nodes', href: '' }
    Object.defineProperty(window, 'location', { value: loc, writable: true, configurable: true })
  })

  it('redirects a business-page 403 to the enrollment page', async () => {
    await expect(captured.onErr!(makeError(403, { must_enroll_mfa: true }))).rejects.toBeTruthy()
    expect(loc.href).toBe('/mfa/enroll')
    // 跳转已是反馈,不再叠一层 toast
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('does not redirect when already on the enrollment page (no loop)', async () => {
    loc.pathname = '/mfa/enroll'
    await expect(captured.onErr!(makeError(403, { must_enroll_mfa: true }))).rejects.toBeTruthy()
    expect(loc.href).toBe('')
  })

  it('leaves an ordinary 403 alone', async () => {
    await expect(captured.onErr!(makeError(403, { error: 'forbidden' }))).rejects.toBeTruthy()
    expect(loc.href).toBe('')
    expect(ElMessage.error).toHaveBeenCalledWith('forbidden')
  })
})
