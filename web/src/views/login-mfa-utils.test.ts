// Unit tests for the second login stage helpers (ticket 09): input
// normalization for both code types and the wire-error wording.
import { describe, it, expect } from 'vitest'
import {
  RECOVERY_CODE_LENGTH,
  TOTP_CODE_LENGTH,
  isCompleteLoginMFACode,
  isLoginMFASessionLost,
  loginMFAErrorMessage,
  normalizeLoginMFACode,
  normalizeRecoveryCode,
  normalizeTOTPCode
} from './login-mfa-utils'

describe('normalizeTOTPCode', () => {
  it('保留数字并截断到 6 位', () => {
    expect(normalizeTOTPCode('123 456')).toBe('123456')
    expect(normalizeTOTPCode('12-34-56')).toBe('123456')
    expect(normalizeTOTPCode('1234567890')).toBe('123456')
    expect(normalizeTOTPCode('abc123')).toBe('123')
  })

  it('空与非法输入归零', () => {
    expect(normalizeTOTPCode('')).toBe('')
    expect(normalizeTOTPCode('abcdef')).toBe('')
  })
})

describe('normalizeRecoveryCode', () => {
  it('大写、去噪并按 4-4-4 分组', () => {
    expect(normalizeRecoveryCode('abcd efgh jkmn')).toBe('ABCD-EFGH-JKMN')
    expect(normalizeRecoveryCode('ABCDEFGHJKMN')).toBe('ABCD-EFGH-JKMN')
    expect(normalizeRecoveryCode('abcd-efgh-jkmn')).toBe('ABCD-EFGH-JKMN')
  })

  it('丢弃字符集外的易混字形（0/O/1/I/L）与超长部分', () => {
    // O/I/L/0/1 都不在 recoveryCharset 里,直接丢弃
    expect(normalizeRecoveryCode('ABCD-EFGH-JKMN-OIL01')).toBe('ABCD-EFGH-JKMN')
    expect(normalizeRecoveryCode('ab')).toBe('AB')
  })

  it('空输入返回空', () => {
    expect(normalizeRecoveryCode('')).toBe('')
    expect(normalizeRecoveryCode('---')).toBe('')
  })
})

describe('normalizeLoginMFACode / isCompleteLoginMFACode', () => {
  it('按码型分派归一化', () => {
    expect(normalizeLoginMFACode('12 34 56', 'totp')).toBe('123456')
    expect(normalizeLoginMFACode('abcdefghjkmn', 'recovery')).toBe('ABCD-EFGH-JKMN')
  })

  it('完整性判定按各自长度', () => {
    expect(isCompleteLoginMFACode('123456', 'totp')).toBe(true)
    expect(isCompleteLoginMFACode('12345', 'totp')).toBe(false)
    expect(isCompleteLoginMFACode('ABCD-EFGH-JKMN', 'recovery')).toBe(true)
    expect(isCompleteLoginMFACode('ABCD-EFGH', 'recovery')).toBe(false)
    // 换了码型就不完整:6 位数字不是恢复码
    expect(isCompleteLoginMFACode('123456', 'recovery')).toBe(false)
  })

  it('长度常量与后端契约一致', () => {
    expect(TOTP_CODE_LENGTH).toBe(6)
    expect(RECOVERY_CODE_LENGTH).toBe('ABCD-EFGH-JKMN'.length)
  })
})

describe('loginMFAErrorMessage', () => {
  const withStatus = (status: number, data: unknown = '') => ({ response: { status, data } })

  it('401 一律给出"重新输入或返回重新登录"的双读文案', () => {
    // 码错、过期、异 IP、超次在协议上不可区分,文案必须同时覆盖两种读法
    const msg = loginMFAErrorMessage(withStatus(401, 'invalid verification code'))
    expect(msg).toContain('验证码错误或已过期')
    expect(msg).toContain('重新登录')
    expect(loginMFAErrorMessage(withStatus(401, 'invalid or expired verification session'))).toBe(
      msg
    )
  })

  it('其余状态各有其文案', () => {
    expect(loginMFAErrorMessage(withStatus(400, { error: 'code required' }))).toBe('请输入验证码')
    expect(loginMFAErrorMessage(withStatus(403, 'account disabled'))).toContain('禁用')
    expect(loginMFAErrorMessage(withStatus(429))).toContain('频繁')
    expect(loginMFAErrorMessage(withStatus(500, 'internal error'))).toBe('internal error')
  })

  it('无响应视为网络异常', () => {
    expect(loginMFAErrorMessage(new Error('boom'))).toBe('网络异常，请检查连接后重试')
    expect(loginMFAErrorMessage(null)).toBe('网络异常，请检查连接后重试')
  })
})

describe('isLoginMFASessionLost', () => {
  it('只有 403 判定为句柄不可复用', () => {
    expect(isLoginMFASessionLost({ response: { status: 403, data: 'account disabled' } })).toBe(
      true
    )
    // 401 保持可重试:多数情况只是打错一个码
    expect(isLoginMFASessionLost({ response: { status: 401, data: 'invalid' } })).toBe(false)
    expect(isLoginMFASessionLost(new Error('boom'))).toBe(false)
  })
})
