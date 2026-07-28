// Unit tests for the audit display helpers: event label/tag mapping, the
// filter option list, and the login_success mfa marker parsing.
import { describe, it, expect } from 'vitest'
import {
  EVENT_FILTER_OPTIONS,
  detailText,
  eventLabel,
  eventTag,
  loginMFABadge,
  mfaMarkerOf
} from './audit-utils'

describe('eventLabel / eventTag', () => {
  it('labels the four login-hardening events in Chinese', () => {
    expect(eventLabel('captcha_failure')).toBe('验证码失败')
    expect(eventLabel('mfa_enrolled')).toBe('MFA 绑定')
    expect(eventLabel('mfa_failure')).toBe('MFA 失败')
    expect(eventLabel('mfa_reset')).toBe('MFA 重置')
  })

  it('keeps the pre-existing event labels unchanged', () => {
    expect(eventLabel('login_success')).toBe('登录成功')
    expect(eventLabel('login_failure')).toBe('登录失败')
    expect(eventLabel('honeypot_ban')).toBe('蜜罐封禁')
    expect(eventLabel('threshold_ban')).toBe('阈值封禁')
  })

  it('falls back to the raw type so future backend events still render', () => {
    expect(eventLabel('trusted_ip_added')).toBe('trusted_ip_added')
    expect(eventTag('trusted_ip_added')).toBe('info')
  })

  it('colors success / warning / danger by severity', () => {
    expect(eventTag('login_success')).toBe('success')
    expect(eventTag('mfa_enrolled')).toBe('success')
    expect(eventTag('captcha_failure')).toBe('warning')
    expect(eventTag('mfa_failure')).toBe('warning')
    expect(eventTag('mfa_reset')).toBe('danger')
    expect(eventTag('threshold_ban')).toBe('danger')
  })
})

describe('EVENT_FILTER_OPTIONS', () => {
  it('offers every mapped event type, new ones included', () => {
    const values = EVENT_FILTER_OPTIONS.map((o) => o.value)
    expect(values).toEqual(
      expect.arrayContaining([
        'login_success',
        'login_failure',
        'captcha_failure',
        'mfa_failure',
        'mfa_enrolled',
        'mfa_reset',
        'honeypot_ban',
        'threshold_ban'
      ])
    )
  })

  it('derives its labels from the same map the table renders', () => {
    for (const opt of EVENT_FILTER_OPTIONS) {
      expect(opt.label).toBe(eventLabel(opt.value))
    }
  })
})

describe('mfaMarkerOf', () => {
  it('reads the three backend markers', () => {
    expect(mfaMarkerOf('mfa=totp')).toBe('totp')
    expect(mfaMarkerOf('mfa=recovery')).toBe('recovery')
    expect(mfaMarkerOf('mfa_skipped=trusted_ip')).toBe('trusted_ip')
  })

  it('finds the marker among other detail fragments', () => {
    expect(mfaMarkerOf('session=abc mfa=totp')).toBe('totp')
    expect(mfaMarkerOf('foo=1, mfa_skipped=trusted_ip')).toBe('trusted_ip')
  })

  it('does not mistake mfa_skipped=trusted_ip for a real second factor', () => {
    // A substring match on "mfa=" would wrongly hit "mfa_skipped=..." here.
    expect(mfaMarkerOf('mfa_skipped=trusted_ip')).not.toBe('totp')
  })

  it('returns null when there is no marker', () => {
    expect(mfaMarkerOf('')).toBeNull()
    expect(mfaMarkerOf('must_change_password')).toBeNull()
    expect(mfaMarkerOf(undefined as unknown as string)).toBeNull()
  })
})

describe('loginMFABadge', () => {
  it('distinguishes a real MFA login from a trusted-IP skip', () => {
    expect(loginMFABadge('login_success', 'mfa=totp')).toMatchObject({
      marker: 'totp',
      label: 'TOTP'
    })
    expect(loginMFABadge('login_success', 'mfa=recovery')).toMatchObject({
      marker: 'recovery',
      label: '恢复码'
    })
    expect(loginMFABadge('login_success', 'mfa_skipped=trusted_ip')).toMatchObject({
      marker: 'trusted_ip',
      label: '受信 IP 免验'
    })
  })

  it('badges nothing for a login_success without a marker (MFA not enrolled)', () => {
    expect(loginMFABadge('login_success', '')).toBeNull()
  })

  it('badges nothing for other event types', () => {
    expect(loginMFABadge('mfa_failure', 'mfa=totp')).toBeNull()
    expect(loginMFABadge('login_failure', 'mfa=totp')).toBeNull()
  })
})

describe('detailText', () => {
  it('strips the marker already shown as a badge', () => {
    expect(detailText('login_success', 'mfa=totp')).toBe('')
    expect(detailText('login_success', 'session=abc mfa=totp')).toBe('session=abc')
    expect(detailText('login_success', 'mfa_skipped=trusted_ip')).toBe('')
  })

  it('leaves other events untouched', () => {
    expect(detailText('captcha_failure', '缺少验证码')).toBe('缺少验证码')
    expect(detailText('mfa_failure', 'mfa verification failed, pending=abcd1234')).toBe(
      'mfa verification failed, pending=abcd1234'
    )
  })

  it('tolerates a missing detail', () => {
    expect(detailText('login_success', undefined as unknown as string)).toBe('')
    expect(detailText('honeypot_ban', undefined as unknown as string)).toBe('')
  })
})
