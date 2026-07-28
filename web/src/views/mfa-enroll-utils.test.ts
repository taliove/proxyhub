// Unit tests for the MFA enrollment helpers: code normalization, the submit
// gate, secret formatting, and the wire-error -> Chinese message mapping.
import { describe, it, expect } from 'vitest'
import {
  TOTP_CODE_LENGTH,
  enrollConfirmErrorMessage,
  enrollStartErrorMessage,
  formatSecretForDisplay,
  isAlreadyEnrolled,
  isCompleteTOTPCode,
  normalizeTOTPCode
} from './mfa-enroll-utils'

const httpError = (status: number, data: unknown = {}) => ({ response: { status, data } })

describe('normalizeTOTPCode', () => {
  it('strips spaces so a pasted "123 456" still verifies', () => {
    expect(normalizeTOTPCode('123 456')).toBe('123456')
  })

  it('drops non-digits and truncates past 6 chars', () => {
    expect(normalizeTOTPCode('a1b2c3d4e5f6g7')).toBe('123456')
    expect(normalizeTOTPCode('1234567890')).toBe('123456')
  })

  it('tolerates empty and nullish input', () => {
    expect(normalizeTOTPCode('')).toBe('')
    expect(normalizeTOTPCode(undefined as unknown as string)).toBe('')
  })
})

describe('isCompleteTOTPCode', () => {
  it('is true only at exactly 6 digits', () => {
    expect(isCompleteTOTPCode('12345')).toBe(false)
    expect(isCompleteTOTPCode('123456')).toBe(true)
    expect(isCompleteTOTPCode('123 456')).toBe(true)
    expect(isCompleteTOTPCode('abcdef')).toBe(false)
  })

  it('agrees with the exported length constant', () => {
    expect(isCompleteTOTPCode('1'.repeat(TOTP_CODE_LENGTH))).toBe(true)
  })
})

describe('formatSecretForDisplay', () => {
  it('groups the base32 secret into 4-char blocks', () => {
    expect(formatSecretForDisplay('ABCDEFGHIJKLMNOP')).toBe('ABCD EFGH IJKL MNOP')
  })

  it('keeps a trailing partial block', () => {
    expect(formatSecretForDisplay('ABCDEFGHIJ')).toBe('ABCD EFGH IJ')
  })

  it('returns empty for empty input', () => {
    expect(formatSecretForDisplay('')).toBe('')
  })
})

describe('enrollStartErrorMessage', () => {
  it('explains the 409 already-enrolled case', () => {
    expect(enrollStartErrorMessage(httpError(409, { error: 'mfa already enrolled' }))).toContain(
      '已绑定'
    )
  })

  it('falls back to a retry hint on other failures', () => {
    expect(enrollStartErrorMessage(httpError(500))).toContain('刷新页面重试')
    expect(enrollStartErrorMessage(new Error('network down'))).toContain('刷新页面重试')
  })
})

describe('isAlreadyEnrolled', () => {
  it('is true only for 409', () => {
    expect(isAlreadyEnrolled(httpError(409))).toBe(true)
    expect(isAlreadyEnrolled(httpError(400))).toBe(false)
    expect(isAlreadyEnrolled(new Error('boom'))).toBe(false)
  })
})

describe('enrollConfirmErrorMessage', () => {
  it('distinguishes a wrong code from a lost staged secret', () => {
    expect(
      enrollConfirmErrorMessage(httpError(400, { error: 'invalid verification code' }))
    ).toContain('验证码不正确')
    expect(
      enrollConfirmErrorMessage(
        httpError(400, { error: 'no pending enrollment, request a secret first' })
      )
    ).toContain('重新获取密钥')
  })

  it('handles 409 and unknown failures', () => {
    expect(enrollConfirmErrorMessage(httpError(409))).toContain('已绑定')
    expect(enrollConfirmErrorMessage(httpError(500))).toContain('稍后重试')
  })
})
