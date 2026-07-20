import { describe, it, expect } from 'vitest'
import {
  regionRowStatus,
  regionStatusText,
  formatTtfb,
  formatMbps,
  upsertRegionRow
} from './regionspeed'
import type { ExamRegionResult } from '@/types'

const row = (code: string, over: Partial<ExamRegionResult> = {}): ExamRegionResult => ({
  code,
  name: code.toUpperCase(),
  ttfb_ms: 40,
  down_mbps: 20,
  ...over
})

describe('regionRowStatus / regionStatusText', () => {
  it('maps error presence to status', () => {
    expect(regionRowStatus(row('a'))).toBe('ok')
    expect(regionRowStatus(row('a', { error: 'boom' }))).toBe('error')
    expect(regionStatusText(row('a'))).toBe('正常')
    expect(regionStatusText(row('a', { error: 'boom' }))).toBe('失败')
  })
})

describe('formatTtfb', () => {
  it('rounds and appends unit', () => {
    expect(formatTtfb(41.6)).toBe('42 ms')
    expect(formatTtfb(0)).toBe('0 ms')
  })
  it('shows placeholder for undefined / non-finite', () => {
    expect(formatTtfb(undefined)).toBe('-')
    expect(formatTtfb(Number.NaN)).toBe('-')
  })
})

describe('formatMbps', () => {
  it('keeps one decimal and appends unit', () => {
    expect(formatMbps(18.53)).toBe('18.5 Mbps')
    expect(formatMbps(0)).toBe('0.0 Mbps')
  })
  it('shows placeholder for undefined / non-finite', () => {
    expect(formatMbps(undefined)).toBe('-')
    expect(formatMbps(Number.POSITIVE_INFINITY)).toBe('-')
  })
})

describe('upsertRegionRow', () => {
  it('appends a new code and preserves order', () => {
    const r1 = upsertRegionRow([], row('a'))
    const r2 = upsertRegionRow(r1, row('b'))
    expect(r2.map((r) => r.code)).toEqual(['a', 'b'])
  })

  it('replaces an existing code in place (immutably)', () => {
    const base = [row('a'), row('b')]
    const next = upsertRegionRow(base, row('a', { down_mbps: 99 }))
    expect(next).not.toBe(base)
    expect(next[0].down_mbps).toBe(99)
    expect(next.map((r) => r.code)).toEqual(['a', 'b'])
    // 原数组未被修改
    expect(base[0].down_mbps).toBe(20)
  })
})
