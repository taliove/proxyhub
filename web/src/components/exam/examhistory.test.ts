import { describe, it, expect } from 'vitest'
import {
  parseTimeMs,
  relativeTimeZh,
  examStabilityScore,
  examUnlockSummary,
  examBadgeSummary,
  buildTimelineItems
} from './examhistory'
import type { ExamHistoryEntry, ExamReport } from '@/types'

// report 构造器:只填被测段,其余留空(omitempty 语义)。
const report = (over: Partial<ExamReport> = {}): ExamReport => ({ ...over })

const entry = (over: Partial<ExamHistoryEntry> = {}): ExamHistoryEntry => ({
  id: 1,
  node_key: 'example.com:443',
  report: report({ stability: metrics(87) }),
  created_at: '2026-07-20T09:00:00Z',
  ...over
})

// 稳定性指标构造器:只有 score 影响展示逻辑,其余置零。
const metrics = (score: number) => ({
  total: 30,
  succeeded: 30,
  loss_rate: 0,
  mean_ms: 100,
  median_ms: 100,
  p95_ms: 120,
  p99_ms: 140,
  jitter_ms: 5,
  score
})

describe('parseTimeMs', () => {
  it('parses RFC3339 into epoch ms', () => {
    expect(parseTimeMs('2026-07-20T00:00:00Z')).toBe(Date.parse('2026-07-20T00:00:00Z'))
  })
  it('returns NaN for empty / invalid', () => {
    expect(Number.isNaN(parseTimeMs(''))).toBe(true)
    expect(Number.isNaN(parseTimeMs(undefined))).toBe(true)
    expect(Number.isNaN(parseTimeMs('not-a-date'))).toBe(true)
  })
})

describe('relativeTimeZh', () => {
  const now = Date.parse('2026-07-20T12:00:00Z')
  it('shows 刚刚 within a minute', () => {
    expect(relativeTimeZh(now - 30_000, now)).toBe('刚刚')
  })
  it('shows minutes / hours / days buckets', () => {
    expect(relativeTimeZh(now - 5 * 60_000, now)).toBe('5分钟前')
    expect(relativeTimeZh(now - 3 * 3600_000, now)).toBe('3小时前')
    expect(relativeTimeZh(now - 2 * 86_400_000, now)).toBe('2天前')
  })
  it('falls back to MM-DD date beyond a week', () => {
    const old = Date.parse('2026-06-01T08:00:00Z')
    expect(relativeTimeZh(old, now)).toMatch(/^\d{2}-\d{2}$/)
  })
  it('clamps clock skew (future) to 刚刚 and guards NaN', () => {
    expect(relativeTimeZh(now + 10_000, now)).toBe('刚刚')
    expect(relativeTimeZh(NaN, now)).toBe('—')
  })
})

describe('examStabilityScore', () => {
  it('reads the stability score when present', () => {
    expect(examStabilityScore(report({ stability: metrics(72) }))).toBe(72)
  })
  it('returns null when the stability section is absent', () => {
    expect(examStabilityScore(report())).toBeNull()
  })
})

describe('examUnlockSummary', () => {
  it('counts full-unlocked streaming plus available binary targets', () => {
    const r = report({
      unlock: {
        results: [
          { node_key: 'n', target_name: 'Netflix', available: true, latency: 1, level: 'full' },
          {
            node_key: 'n',
            target_name: 'Disney+',
            available: false,
            latency: 1,
            level: 'blocked'
          },
          { node_key: 'n', target_name: 'OpenAI', available: true, latency: 1 }
        ]
      }
    })
    expect(examUnlockSummary(r)).toBe('解锁 2/3')
  })
  it('returns empty string when there are no unlock results', () => {
    expect(examUnlockSummary(report())).toBe('')
    expect(examUnlockSummary(report({ unlock: { results: [] } }))).toBe('')
  })
})

describe('examBadgeSummary', () => {
  const now = Date.parse('2026-07-20T12:00:00Z')
  it('returns null for no history (badge must not occupy space)', () => {
    expect(examBadgeSummary(null, now)).toBeNull()
  })
  it('returns null when the latest report has no stability score', () => {
    expect(examBadgeSummary(entry({ report: report() }), now)).toBeNull()
  })
  it('renders 稳定性 <score> · <relative>', () => {
    const badge = examBadgeSummary(
      entry({ report: report({ stability: metrics(87) }), created_at: '2026-07-20T09:00:00Z' }),
      now
    )
    expect(badge).not.toBeNull()
    expect(badge?.score).toBe(87)
    expect(badge?.level).toBe('good')
    expect(badge?.text).toBe('稳定性 87 · 3小时前')
  })
})

describe('buildTimelineItems', () => {
  const now = Date.parse('2026-07-20T12:00:00Z')
  it('maps entries to view rows (id / relative / score / unlock summary)', () => {
    const items = buildTimelineItems(
      [
        entry({
          id: 9,
          created_at: '2026-07-20T11:00:00Z',
          report: report({
            stability: metrics(90),
            unlock: {
              results: [
                {
                  node_key: 'n',
                  target_name: 'Netflix',
                  available: true,
                  latency: 1,
                  level: 'full'
                }
              ]
            }
          })
        })
      ],
      now
    )
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({
      id: 9,
      relative: '1小时前',
      score: 90,
      scoreLevel: 'good',
      unlockSummary: '解锁 1/1'
    })
  })
  it('yields an empty array for empty history (drives the guide state)', () => {
    expect(buildTimelineItems([], now)).toEqual([])
  })
  it('tolerates entries whose report lacks a stability section', () => {
    const items = buildTimelineItems([entry({ report: report() })], now)
    expect(items[0].score).toBeNull()
    expect(items[0].scoreLevel).toBeNull()
  })
})
