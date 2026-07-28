// 设置页校验助手的单测:验证码触发次数必须是 0..MAX 的整数(0 合法)。
import { describe, it, expect } from 'vitest'
import {
  CAPTCHA_TRIGGER_THRESHOLD_DEFAULT,
  CAPTCHA_TRIGGER_THRESHOLD_MAX,
  validateCaptchaTriggerThreshold
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
