import { describe, it, expect } from 'vitest'
import { captchaRequiredFromError, loginErrorMessage } from './login-utils'

const axiosErr = (status: number, data: unknown) => ({ response: { status, data } })

describe('captchaRequiredFromError', () => {
  it('JSON 体带 captcha_required=true 时为真', () => {
    expect(
      captchaRequiredFromError(
        axiosErr(401, { error: 'invalid credentials', captcha_required: true })
      )
    ).toBe(true)
  })

  it('纯文本 401(http.Error)不带标记', () => {
    expect(captchaRequiredFromError(axiosErr(401, 'invalid credentials\n'))).toBe(false)
  })

  it('无响应(网络错误)不带标记', () => {
    expect(captchaRequiredFromError(new Error('network'))).toBe(false)
  })
})

describe('loginErrorMessage', () => {
  it('验证码错误单独提示', () => {
    expect(
      loginErrorMessage(axiosErr(401, { error: 'captcha required', captcha_required: true }))
    ).toContain('验证码')
  })

  it('密码错且需要验证码时提示补验证码', () => {
    const msg = loginErrorMessage(
      axiosErr(401, { error: 'invalid credentials', captcha_required: true })
    )
    expect(msg).toContain('密码错误')
    expect(msg).toContain('验证码')
  })

  it('单纯密码错不提验证码', () => {
    expect(loginErrorMessage(axiosErr(401, 'invalid credentials\n'))).toBe('用户名或密码错误')
  })

  it('账号禁用与封禁分别提示', () => {
    expect(loginErrorMessage(axiosErr(403, 'account disabled\n'))).toContain('禁用')
    expect(loginErrorMessage(axiosErr(403, 'too many failed attempts, try later\n'))).toContain(
      '封禁'
    )
  })

  it('429 节流与网络异常有独立文案', () => {
    expect(loginErrorMessage(axiosErr(429, 'too many captcha requests\n'))).toContain('频繁')
    expect(loginErrorMessage(new Error('network'))).toContain('网络')
  })
})
