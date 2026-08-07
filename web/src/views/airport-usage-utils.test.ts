import { describe, it, expect } from 'vitest'
import type { Airport } from '@/types'
import {
  formatBytes,
  usageRemaining,
  usageRemainingPercent,
  isUsageLow,
  expireText,
  isExpiringSoon,
  isExpired,
  gbToBytes,
  bytesToGb,
  usageFormFromAirport,
  usageFormToPayload,
  usageFormToPayloadOrZero,
  GIB
} from './airport-utils'

const base: Airport = {
  id: 1,
  name: '测试机场',
  url: 'https://example.com/sub',
  abbr: '',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z'
}

describe('airport usage utils', () => {
  it('formatBytes 人性化字节', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(GIB)).toBe('1.0 GB')
    expect(formatBytes(1.5 * GIB)).toBe('1.5 GB')
    expect(formatBytes(2 * GIB * 1024)).toBe('2.0 TB')
  })

  it('usageRemaining/Percent:剩余 = 总量 - 上行 - 下行', () => {
    const a: Airport = { ...base, usage_upload: 100, usage_download: 200, usage_total: 1000 }
    expect(usageRemaining(a)).toBe(700)
    expect(usageRemainingPercent(a)).toBe(70)
  })

  it('未知用量：总量为 0 返回 null(不展示)', () => {
    expect(usageRemaining(base)).toBeNull()
    expect(usageRemainingPercent(base)).toBeNull()
    expect(isUsageLow(base)).toBe(false)
  })

  it('用量为负已用时剩余钳制为总量', () => {
    const a: Airport = { ...base, usage_total: 1000 }
    expect(usageRemaining(a)).toBe(1000)
  })

  it('isUsageLow:剩余 <10% 标红', () => {
    const low: Airport = { ...base, usage_download: 950, usage_total: 1000 }
    expect(isUsageLow(low)).toBe(true)
    const ok: Airport = { ...base, usage_download: 500, usage_total: 1000 }
    expect(isUsageLow(ok)).toBe(false)
  })

  it('过期：expireText/临期/已过期', () => {
    const exp = Math.floor(new Date('2030-06-15T00:00:00').getTime() / 1000)
    const a: Airport = { ...base, usage_expire: exp }
    expect(expireText(a)).toBe('2030-06-15')

    const soon = Math.floor((Date.now() + 3 * 24 * 3600 * 1000) / 1000)
    expect(isExpiringSoon({ ...base, usage_expire: soon })).toBe(true)
    const far = Math.floor((Date.now() + 30 * 24 * 3600 * 1000) / 1000)
    expect(isExpiringSoon({ ...base, usage_expire: far })).toBe(false)

    const past = Math.floor((Date.now() - 3600 * 1000) / 1000)
    expect(isExpired({ ...base, usage_expire: past })).toBe(true)
    expect(expireText(base)).toBeNull()
  })

  it('gb/bytes 互转', () => {
    expect(gbToBytes(1)).toBe(GIB)
    expect(gbToBytes(null)).toBe(0)
    expect(gbToBytes(-5)).toBe(0)
    expect(bytesToGb(GIB)).toBe(1)
    expect(bytesToGb(0)).toBeNull()
  })

  it('usageForm 与机场行互转', () => {
    const exp = Math.floor(new Date('2030-06-15T00:00:00').getTime() / 1000)
    const a: Airport = {
      ...base,
      source_type: 'manual',
      usage_download: 0.5 * GIB,
      usage_total: 2 * GIB,
      usage_expire: exp,
      web_page_url: 'https://example.com'
    }
    const form = usageFormFromAirport(a)
    expect(form.totalGb).toBe(2)
    expect(form.remainingGb).toBe(1.5)
    expect(form.expireDate).toBe('2030-06-15')
    expect(form.webPageUrl).toBe('https://example.com')

    const payload = usageFormToPayload(form)
    expect(payload).not.toBeNull()
    expect(payload?.usage_total).toBe(2 * GIB)
    expect(payload?.usage_remaining).toBe(1.5 * GIB)
    expect(payload?.usage_expire).toBeGreaterThan(0)
    expect(payload?.web_page_url).toBe('https://example.com')
  })

  it('usageFormToPayload:全空返回 null(不提供，不动既有值)', () => {
    expect(
      usageFormToPayload({ remainingGb: null, totalGb: null, expireDate: '', webPageUrl: '' })
    ).toBeNull()
  })

  it('usageFormToPayloadOrZero:全空发零值（编辑显式清空可达后端）', () => {
    const payload = usageFormToPayloadOrZero({
      remainingGb: null,
      totalGb: null,
      expireDate: '',
      webPageUrl: ''
    })
    expect(payload).toEqual({
      usage_remaining: 0,
      usage_total: 0,
      usage_expire: 0,
      web_page_url: ''
    })
  })
})
