// 设置页校验助手的单测:验证码触发次数必须是 0..MAX 的整数(0 合法)。
import { describe, it, expect } from 'vitest'
import {
  CAPTCHA_TRIGGER_THRESHOLD_DEFAULT,
  CAPTCHA_TRIGGER_THRESHOLD_MAX,
  validateCaptchaTriggerThreshold,
  PULL_RATE_LIMIT_DEFAULT,
  PULL_RATE_LIMIT_MAX,
  validatePullRateLimit,
  PULL_BLACKLIST_ESCALATION_DEFAULT,
  validatePullBlacklistEscalation,
  PULL_BLACKLIST_DURATION_DEFAULT,
  validatePullBlacklistDuration
} from './settings-utils'

describe('validateCaptchaTriggerThreshold', () => {
  it('接受非负整数,0 表示每次登录都要求验证码', () => {
    expect(validateCaptchaTriggerThreshold(0)).toBeNull()
    expect(validateCaptchaTriggerThreshold(1)).toBeNull()
    expect(validateCaptchaTriggerThreshold(CAPTCHA_TRIGGER_THRESHOLD_MAX)).toBeNull()
    // 后端读的是字符串(map[string]string),字符串形态同样要放行
    expect(validateCaptchaTriggerThreshold('3')).toBeNull()
    expect(validateCaptchaTriggerThreshold(' 3 ')).toBeNull()
  })

  it('拒绝负数', () => {
    expect(validateCaptchaTriggerThreshold(-1)).toContain('负数')
    expect(validateCaptchaTriggerThreshold('-2')).toContain('负数')
  })

  it('拒绝非整数与非数字', () => {
    expect(validateCaptchaTriggerThreshold(1.5)).toContain('整数')
    expect(validateCaptchaTriggerThreshold('abc')).toContain('整数')
    expect(validateCaptchaTriggerThreshold(Number.NaN)).toContain('整数')
  })

  it('拒绝空值(留空等于让后端静默回落默认值)', () => {
    expect(validateCaptchaTriggerThreshold('')).toContain('不能为空')
    expect(validateCaptchaTriggerThreshold('   ')).toContain('不能为空')
    expect(validateCaptchaTriggerThreshold(null)).toContain('不能为空')
    expect(validateCaptchaTriggerThreshold(undefined)).toContain('不能为空')
  })

  it('拒绝超过上限的取值', () => {
    expect(validateCaptchaTriggerThreshold(CAPTCHA_TRIGGER_THRESHOLD_MAX + 1)).toContain('不能大于')
  })

  it('默认值与后端 defaultCaptchaTriggerThreshold 对齐(1)', () => {
    expect(CAPTCHA_TRIGGER_THRESHOLD_DEFAULT).toBe(1)
    expect(validateCaptchaTriggerThreshold(CAPTCHA_TRIGGER_THRESHOLD_DEFAULT)).toBeNull()
  })
})

describe('validatePullRateLimit', () => {
  it('接受非负整数,0 表示关闭限频', () => {
    expect(validatePullRateLimit(0)).toBeNull()
    expect(validatePullRateLimit(1)).toBeNull()
    expect(validatePullRateLimit(60)).toBeNull()
    expect(validatePullRateLimit(PULL_RATE_LIMIT_MAX)).toBeNull()
    expect(validatePullRateLimit('100')).toBeNull()
    expect(validatePullRateLimit(' 60 ')).toBeNull()
  })

  it('拒绝负数', () => {
    expect(validatePullRateLimit(-1)).toContain('负数')
    expect(validatePullRateLimit('-5')).toContain('负数')
  })

  it('拒绝非整数与非数字', () => {
    expect(validatePullRateLimit(1.5)).toContain('整数')
    expect(validatePullRateLimit('abc')).toContain('整数')
    expect(validatePullRateLimit(Number.NaN)).toContain('整数')
  })

  it('拒绝空值', () => {
    expect(validatePullRateLimit('')).toContain('不能为空')
    expect(validatePullRateLimit('   ')).toContain('不能为空')
    expect(validatePullRateLimit(null)).toContain('不能为空')
    expect(validatePullRateLimit(undefined)).toContain('不能为空')
  })

  it('拒绝超过上限的取值', () => {
    expect(validatePullRateLimit(PULL_RATE_LIMIT_MAX + 1)).toContain('不能大于')
  })

  it('默认值与后端 defaultPullRateLimitPerHour 对齐(60)', () => {
    expect(PULL_RATE_LIMIT_DEFAULT).toBe(60)
    expect(validatePullRateLimit(PULL_RATE_LIMIT_DEFAULT)).toBeNull()
  })
})

describe('validatePullBlacklistEscalation', () => {
  it('接受正整数', () => {
    expect(validatePullBlacklistEscalation(1)).toBeNull()
    expect(validatePullBlacklistEscalation(10)).toBeNull()
    expect(validatePullBlacklistEscalation(100)).toBeNull()
    expect(validatePullBlacklistEscalation('5')).toBeNull()
    expect(validatePullBlacklistEscalation(' 10 ')).toBeNull()
  })

  it('拒绝 0 和负数', () => {
    expect(validatePullBlacklistEscalation(0)).toContain('正数')
    expect(validatePullBlacklistEscalation(-1)).toContain('正数')
    expect(validatePullBlacklistEscalation('-5')).toContain('正数')
  })

  it('拒绝非整数与非数字', () => {
    expect(validatePullBlacklistEscalation(1.5)).toContain('整数')
    expect(validatePullBlacklistEscalation('abc')).toContain('整数')
    expect(validatePullBlacklistEscalation(Number.NaN)).toContain('整数')
  })

  it('拒绝空值', () => {
    expect(validatePullBlacklistEscalation('')).toContain('不能为空')
    expect(validatePullBlacklistEscalation('   ')).toContain('不能为空')
    expect(validatePullBlacklistEscalation(null)).toContain('不能为空')
    expect(validatePullBlacklistEscalation(undefined)).toContain('不能为空')
  })

  it('默认值与后端 defaultPullBlacklistEscalationCount 对齐(10)', () => {
    expect(PULL_BLACKLIST_ESCALATION_DEFAULT).toBe(10)
    expect(validatePullBlacklistEscalation(PULL_BLACKLIST_ESCALATION_DEFAULT)).toBeNull()
  })
})

describe('validatePullBlacklistDuration', () => {
  it('接受合法的 Go duration 格式', () => {
    expect(validatePullBlacklistDuration('1h')).toBeNull()
    expect(validatePullBlacklistDuration('24h')).toBeNull()
    expect(validatePullBlacklistDuration('168h')).toBeNull()
    expect(validatePullBlacklistDuration('1.5h')).toBeNull()
    expect(validatePullBlacklistDuration('30m')).toBeNull()
    expect(validatePullBlacklistDuration('90s')).toBeNull()
    expect(validatePullBlacklistDuration(' 24h ')).toBeNull()
  })

  it('拒绝格式错误的时长', () => {
    expect(validatePullBlacklistDuration('24')).toContain('格式错误')
    expect(validatePullBlacklistDuration('abc')).toContain('格式错误')
    expect(validatePullBlacklistDuration('24hours')).toContain('格式错误')
    expect(validatePullBlacklistDuration('h24')).toContain('格式错误')
  })

  it('拒绝空值', () => {
    expect(validatePullBlacklistDuration('')).toContain('不能为空')
    expect(validatePullBlacklistDuration('   ')).toContain('不能为空')
    expect(validatePullBlacklistDuration(null)).toContain('不能为空')
    expect(validatePullBlacklistDuration(undefined)).toContain('不能为空')
  })

  it('默认值与后端 defaultPullBlacklistDuration 对齐(24h)', () => {
    expect(PULL_BLACKLIST_DURATION_DEFAULT).toBe('24h')
    expect(validatePullBlacklistDuration(PULL_BLACKLIST_DURATION_DEFAULT)).toBeNull()
  })
})
