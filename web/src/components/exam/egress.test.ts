import { describe, it, expect } from 'vitest'
import {
  mergeEgress,
  ipv4Location,
  ipv4Asn,
  hostingBadge,
  proxyBadge,
  ipv6Text,
  ipv6Tone,
  dnsText,
  dnsLeakBadge
} from './egress'
import type { ExamEgressMetrics, ExamEgressIPv4, ExamEgressIPv6, ExamEgressDNS } from '@/types'

const ipv4 = (over: Partial<ExamEgressIPv4> = {}): ExamEgressIPv4 => ({
  ip: '203.0.113.7',
  country: 'United States',
  country_code: 'US',
  region: 'California',
  city: 'Los Angeles',
  asn: 'AS64500',
  org: 'Example Hosting',
  proxy: false,
  hosting: true,
  ...over
})

describe('mergeEgress', () => {
  it('overlays incoming sub-fields immutably', () => {
    const base: ExamEgressMetrics = { ipv4: ipv4() }
    const next = mergeEgress(base, { ipv6: { available: false } })
    expect(next).not.toBe(base)
    expect(next.ipv4).toBe(base.ipv4)
    expect(next.ipv6).toEqual({ available: false })
    // 原对象未被修改
    expect(base.ipv6).toBeUndefined()
  })

  it('replaces an existing sub-field when re-sent (authoritative)', () => {
    const base: ExamEgressMetrics = { dns: { resolver_ip: '198.51.100.9', leak: false } }
    const next = mergeEgress(base, { dns: { resolver_ip: '198.51.100.9', leak: true } })
    expect(next.dns?.leak).toBe(true)
    expect(base.dns?.leak).toBe(false)
  })

  it('treats null prev as empty', () => {
    const next = mergeEgress(null, { ipv4: ipv4() })
    expect(next.ipv4?.ip).toBe('203.0.113.7')
  })
})

describe('ipv4Location / ipv4Asn', () => {
  it('joins present country/region/city, skipping empties', () => {
    expect(ipv4Location(ipv4())).toBe('United States · California · Los Angeles')
    expect(ipv4Location(ipv4({ region: '', city: '' }))).toBe('United States')
  })
  it('joins asn and org', () => {
    expect(ipv4Asn(ipv4())).toBe('AS64500 Example Hosting')
    expect(ipv4Asn(ipv4({ org: '' }))).toBe('AS64500')
    expect(ipv4Asn(ipv4({ asn: '', org: 'Example Hosting' }))).toBe('Example Hosting')
  })
})

describe('hostingBadge / proxyBadge', () => {
  it('marks hosting IP as 机房 and residential as 住宅', () => {
    expect(hostingBadge(ipv4({ hosting: true }))?.text).toBe('机房')
    expect(hostingBadge(ipv4({ hosting: false }))?.text).toBe('住宅')
  })
  it('suppresses hosting badge on error result', () => {
    expect(hostingBadge(ipv4({ error: 'boom', hosting: false }))).toBeNull()
  })
  it('shows proxy badge only when proxy flag set', () => {
    expect(proxyBadge(ipv4({ proxy: true }))).toBe(true)
    expect(proxyBadge(ipv4({ proxy: false }))).toBe(false)
  })
})

describe('ipv6Text / ipv6Tone', () => {
  it('shows address when available', () => {
    const v: ExamEgressIPv6 = { available: true, address: '2001:db8::1' }
    expect(ipv6Text(v)).toBe('2001:db8::1')
    expect(ipv6Tone(v)).toBe('ok')
  })
  it('says no egress when unreachable (available false, no error)', () => {
    const v: ExamEgressIPv6 = { available: false }
    expect(ipv6Text(v)).toBe('无 IPv6 出口')
    expect(ipv6Tone(v)).toBe('muted')
  })
  it('distinguishes a probe error from unreachable', () => {
    const v: ExamEgressIPv6 = { available: false, error: '无法解析 IPv6 出口响应' }
    expect(ipv6Text(v)).toBe('探测异常')
    expect(ipv6Tone(v)).toBe('error')
  })
})

describe('dnsText / dnsLeakBadge', () => {
  it('shows resolver ip and geo', () => {
    const v: ExamEgressDNS = {
      resolver_ip: '198.51.100.9',
      resolver_geo: 'United States - Example DNS',
      leak: false
    }
    expect(dnsText(v)).toBe('198.51.100.9 · United States - Example DNS')
    expect(dnsLeakBadge(v)).toBe(false)
  })
  it('flags a suspected leak', () => {
    const v: ExamEgressDNS = { resolver_ip: '198.51.100.9', resolver_geo: 'Germany', leak: true }
    expect(dnsLeakBadge(v)).toBe(true)
  })
  it('shows error text on probe failure', () => {
    expect(dnsText({ leak: false, error: '请求失败' })).toBe('探测异常')
  })
})
