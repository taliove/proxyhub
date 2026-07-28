// 拉取守卫展示口径的单测(pull-guard ticket 06):状态/scope/来源的中英映射与
// 时长 -> 请求体的转换。这些映射的键必须覆盖后端闭集,漏一个就会在页面上漏出英文。
import { describe, it, expect } from 'vitest'
import {
  RULE_DURATION_OPTIONS,
  RULE_DURATION_PERMANENT,
  RULE_SCOPE_OPTIONS,
  pullStatusLabel,
  pullStatusTag,
  ruleScopeLabel,
  ruleScopeTag,
  ruleSourceLabel,
  ruleSourceTag,
  ruleWindowPayload
} from './pullguard'

describe('pullStatusLabel', () => {
  // 键即 internal/store/pull_status.go 的闭集,逐个钉住中文文案
  it.each([
    ['ok', '成功'],
    ['rate_limited', '限频'],
    ['geo_blocked', '地域拦截'],
    ['geo_would_block', '地域观察'],
    ['blacklisted', '黑名单'],
    ['disabled', '已禁用'],
    ['bad_token', '错误令牌']
  ])('maps %s to %s', (status, label) => {
    expect(pullStatusLabel(status)).toBe(label)
  })

  it('falls back to 未知 for an empty status (rows written before ticket 01)', () => {
    expect(pullStatusLabel('')).toBe('未知')
  })

  it('passes an unknown status through so a new backend status stays readable', () => {
    expect(pullStatusLabel('quota_exceeded')).toBe('quota_exceeded')
  })
})

describe('pullStatusTag', () => {
  it('greens only a real delivery and reds the hard blocks', () => {
    expect(pullStatusTag('ok')).toBe('success')
    expect(pullStatusTag('geo_blocked')).toBe('danger')
    expect(pullStatusTag('blacklisted')).toBe('danger')
    expect(pullStatusTag('bad_token')).toBe('danger')
  })

  it('warns on rate limit and on observe-mode geo (served but flagged)', () => {
    expect(pullStatusTag('rate_limited')).toBe('warning')
    expect(pullStatusTag('geo_would_block')).toBe('warning')
  })

  it('uses the neutral colour for disabled and for unknown values', () => {
    expect(pullStatusTag('disabled')).toBe('info')
    expect(pullStatusTag('')).toBe('info')
    expect(pullStatusTag('quota_exceeded')).toBe('info')
  })
})

describe('rule scope mapping', () => {
  it('renders the two scopes with the backend audit wording', () => {
    // 与 internal/server/handlers_iprules.go 的 ipRuleScopeLabel 对齐
    expect(ruleScopeLabel('global')).toBe('整站拒止')
    expect(ruleScopeLabel('sub')).toBe('拉取黑名单')
  })

  it('marks a site-wide block as the heavier action', () => {
    expect(ruleScopeTag('global')).toBe('danger')
    expect(ruleScopeTag('sub')).toBe('warning')
  })

  it('passes unknown scopes through', () => {
    expect(ruleScopeLabel('elsewhere')).toBe('elsewhere')
    expect(ruleScopeTag('elsewhere')).toBe('info')
  })

  it('derives the form options from the same table', () => {
    expect(RULE_SCOPE_OPTIONS.map((o) => o.value)).toEqual(['global', 'sub'])
    expect(RULE_SCOPE_OPTIONS.map((o) => o.label)).toEqual(['整站拒止', '拉取黑名单'])
  })
})

describe('rule source mapping', () => {
  it('renders manual and auto in Chinese', () => {
    expect(ruleSourceLabel('manual')).toBe('手动')
    expect(ruleSourceLabel('auto')).toBe('自动')
  })

  it('flags auto rules so an operator notices the guard wrote them', () => {
    expect(ruleSourceTag('manual')).toBe('info')
    expect(ruleSourceTag('auto')).toBe('warning')
  })

  it('passes unknown sources through', () => {
    expect(ruleSourceLabel('imported')).toBe('imported')
    expect(ruleSourceTag('imported')).toBe('info')
  })
})

describe('ruleWindowPayload', () => {
  it('sends a Go duration string for a bounded window', () => {
    expect(ruleWindowPayload('1h')).toEqual({ duration: '1h' })
    expect(ruleWindowPayload('24h')).toEqual({ duration: '24h' })
  })

  it('sends permanent=true instead of the sentinel string', () => {
    // 后端也认 duration="permanent",但显式布尔不依赖那条兼容路径
    expect(ruleWindowPayload(RULE_DURATION_PERMANENT)).toEqual({ permanent: true })
  })

  it('treats a missing duration as permanent rather than sending neither field', () => {
    // duration 与 permanent 都不给会被后端 400,所以空值必须落到一种形状
    expect(ruleWindowPayload('')).toEqual({ permanent: true })
  })

  it('offers the same duration ladder as the audit ban drawer', () => {
    expect(RULE_DURATION_OPTIONS.map((o) => o.value)).toEqual(['1h', '24h', 'permanent'])
  })
})
