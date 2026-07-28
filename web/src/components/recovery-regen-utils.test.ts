// 恢复码重新生成的错误文案映射单测:后端三种 400 语义必须落到不同引导。
import { describe, it, expect } from 'vitest'
import { regenerateErrorMessage } from './recovery-regen-utils'

const wireError = (status: number, error: string) => ({ response: { status, data: { error } } })

describe('regenerateErrorMessage', () => {
  it('区分"没绑定过 MFA"与"码不对"', () => {
    expect(regenerateErrorMessage(wireError(400, 'mfa is not enrolled'))).toContain('还未绑定')
    expect(regenerateErrorMessage(wireError(400, 'invalid confirmation code'))).toContain(
      '不正确或已过期'
    )
  })

  it('缺码时引导用户填码', () => {
    expect(regenerateErrorMessage(wireError(400, 'confirmation code is required'))).toContain(
      '请输入'
    )
  })

  it('其它状态码落到通用重试文案', () => {
    expect(regenerateErrorMessage(wireError(500, 'internal error'))).toContain('稍后重试')
    expect(regenerateErrorMessage(new Error('network down'))).toContain('稍后重试')
    expect(regenerateErrorMessage(undefined)).toContain('稍后重试')
  })

  it('兼容 {message} 字段(后端错误字段不统一)', () => {
    expect(
      regenerateErrorMessage({
        response: { status: 400, data: { message: 'mfa is not enrolled' } }
      })
    ).toContain('还未绑定')
  })
})
