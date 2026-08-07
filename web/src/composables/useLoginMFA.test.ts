// Unit tests for the second login stage state machine (ticket 09). The view
// test (views/Login.test.ts) covers the rendered flow; this file pins the
// paths that are awkward to reach through the DOM: a dormant submit, the
// incomplete-code gate, and mode switching.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage } from 'element-plus'
import { useLoginMFA } from './useLoginMFA'
import { submitLoginMFA } from '@/api/auth'

vi.mock('@/api/auth', () => ({ submitLoginMFA: vi.fn() }))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

describe('useLoginMFA', () => {
  beforeEach(() => vi.clearAllMocks())

  it('初始休眠：不激活，提交是空操作', async () => {
    const mfa = useLoginMFA()
    expect(mfa.active.value).toBe(false)
    expect(await mfa.submit()).toBeNull()
    expect(vi.mocked(submitLoginMFA)).not.toHaveBeenCalled()
  })

  it('start 激活并复位输入/码型/信任勾选', () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    expect(mfa.active.value).toBe(true)
    expect(mfa.pendingToken.value).toBe('tok')
    expect(mfa.mode.value).toBe('totp')
    expect(mfa.code.value).toBe('')
    expect(mfa.trustIP.value).toBe(false)
  })

  it('码不完整时本地拦截：只提示，不消耗 5 次预算', async () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.setCode('123')
    expect(mfa.codeComplete.value).toBe(false)
    expect(await mfa.submit()).toBeNull()
    expect(vi.mocked(submitLoginMFA)).not.toHaveBeenCalled()
    expect(vi.mocked(ElMessage.warning)).toHaveBeenCalled()
  })

  it('setCode 是唯一写入口：按当前码型归一化', () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.setCode('12 34 56')
    expect(mfa.code.value).toBe('123456')

    mfa.switchMode('recovery')
    mfa.setCode('abcd efgh jkmn')
    expect(mfa.code.value).toBe('ABCD-EFGH-JKMN')
    expect(mfa.codeComplete.value).toBe(true)
  })

  it('切换码型清空输入；重复切换同一码型是空操作', () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.setCode('123456')

    mfa.toggleMode()
    expect(mfa.mode.value).toBe('recovery')
    expect(mfa.code.value).toBe('')

    mfa.setCode('ABCD-EFGH-JKMN')
    mfa.switchMode('recovery')
    expect(mfa.code.value).toBe('ABCD-EFGH-JKMN')
  })

  it('reset 退回休眠并丢弃信任偏好', () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.trustIP.value = true
    mfa.setCode('123456')

    mfa.reset()
    expect(mfa.active.value).toBe(false)
    expect(mfa.pendingToken.value).toBe('')
    expect(mfa.code.value).toBe('')
    expect(mfa.trustIP.value).toBe(false)
  })

  it('401 保留句柄可重试并清空码；403 直接退回休眠', async () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.setCode('123456')

    vi.mocked(submitLoginMFA).mockRejectedValueOnce({ response: { status: 401, data: 'bad' } })
    expect(await mfa.submit()).toBeNull()
    expect(mfa.active.value).toBe(true)
    expect(mfa.code.value).toBe('')
    expect(vi.mocked(ElMessage.error)).toHaveBeenCalled()

    mfa.setCode('123456')
    vi.mocked(submitLoginMFA).mockRejectedValueOnce({ response: { status: 403, data: 'disabled' } })
    expect(await mfa.submit()).toBeNull()
    expect(mfa.active.value).toBe(false)
  })

  it('成功时原样返回登录响应，submitting 归位', async () => {
    const mfa = useLoginMFA()
    mfa.start('tok')
    mfa.setCode('123456')
    vi.mocked(submitLoginMFA).mockResolvedValueOnce({ ok: true, user: { role: 'admin' } })

    const data = await mfa.submit()
    expect(data?.user?.role).toBe('admin')
    expect(mfa.submitting.value).toBe(false)
  })
})
