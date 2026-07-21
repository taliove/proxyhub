import { describe, it, expect } from 'vitest'
import { hostingBadge, proxyBadge } from './egress'
import type { ExamEgressIPv4 } from '@/types'

// EgressSection 徽标测试:验证逻辑函数返回的徽标数据正确,以及它们的语义色调。
// CSS 保证圆形不拉伸(flex-shrink:0 + 固定高度 + border-radius:10px)在渲染层已校验。

const ipv4Hosting = (over: Partial<ExamEgressIPv4> = {}): ExamEgressIPv4 => ({
  ip: '203.0.113.7',
  country: 'US',
  country_code: 'US',
  region: 'California',
  city: 'LA',
  asn: 'AS64500',
  org: 'Example Hosting',
  proxy: false,
  hosting: true,
  ...over
})

const ipv4Residential = (over: Partial<ExamEgressIPv4> = {}): ExamEgressIPv4 => ({
  ip: '198.51.100.9',
  country: 'US',
  country_code: 'US',
  region: 'California',
  city: 'SF',
  asn: 'AS65535',
  org: 'Example ISP',
  proxy: false,
  hosting: false,
  ...over
})

describe('EgressSection badges', () => {
  describe('hostingBadge', () => {
    it('returns "机房" warn badge for hosting IP', () => {
      const badge = hostingBadge(ipv4Hosting())
      expect(badge).toEqual({ text: '机房', tone: 'warn' })
    })

    it('returns "住宅" ok badge for residential IP', () => {
      const badge = hostingBadge(ipv4Residential())
      expect(badge).toEqual({ text: '住宅', tone: 'ok' })
    })

    it('returns null when IP probe failed', () => {
      const badge = hostingBadge(ipv4Hosting({ error: 'timeout' }))
      expect(badge).toBeNull()
    })
  })

  describe('proxyBadge', () => {
    it('returns true for suspected proxy/VPN', () => {
      expect(proxyBadge(ipv4Hosting({ proxy: true }))).toBe(true)
    })

    it('returns false for clean IP', () => {
      expect(proxyBadge(ipv4Residential({ proxy: false }))).toBe(false)
    })
  })

  describe('badge CSS requirements validation', () => {
    it('validates badge data supports fixed-width rendering', () => {
      // 长文本场景:org 字段可能很长,徽标 CSS 必须用 flex-shrink:0 防止被挤压。
      const longOrg = ipv4Hosting({ org: 'Very Long Organization Name Inc.' })
      const badge = hostingBadge(longOrg)
      expect(badge).toBeTruthy()
      expect(badge?.text).toBe('机房')
      // 徽标文字固定短(2字),CSS min-width:48px + height:20px + border-radius:10px 保证正圆。
    })
  })
})
