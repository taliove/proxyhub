import { describe, it, expect } from 'vitest'
import {
  EXAM_SAMPLE_TOTAL,
  EXAM_REGION_TOTAL,
  EXAM_UNLOCK_TOTAL,
  EXAM_TOTAL_UNITS,
  examProgressPercent,
  examActiveSegment,
  examSegmentCounter
} from './progress'
import type { ExamProgressCounts } from './progress'

const counts = (over: Partial<ExamProgressCounts> = {}): ExamProgressCounts => ({
  samples: 0,
  regions: 0,
  unlocks: 0,
  stabilityDone: false,
  ...over
})

describe('exam totals', () => {
  it('weights match the three-segment structure', () => {
    expect(EXAM_SAMPLE_TOTAL).toBe(30)
    expect(EXAM_REGION_TOTAL).toBe(8)
    expect(EXAM_UNLOCK_TOTAL).toBe(6)
    expect(EXAM_TOTAL_UNITS).toBe(44)
  })
})

describe('examProgressPercent', () => {
  it('is 0 at start and 100 when all units received', () => {
    expect(examProgressPercent(counts())).toBe(0)
    expect(examProgressPercent(counts({ samples: 30, regions: 8, unlocks: 6 }))).toBe(100)
  })

  it('weights each received item as one unit out of 44', () => {
    // 22 units of 44 -> 50%
    expect(examProgressPercent(counts({ samples: 22 }))).toBe(50)
    // 30 samples = 68.18.. -> rounded 68
    expect(examProgressPercent(counts({ samples: 30 }))).toBe(68)
    // 30 + 8 = 38/44 = 86.36 -> 86
    expect(examProgressPercent(counts({ samples: 30, regions: 8 }))).toBe(86)
  })

  it('clamps over-count and negatives', () => {
    expect(examProgressPercent(counts({ samples: 99, regions: 99, unlocks: 99 }))).toBe(100)
    expect(examProgressPercent(counts({ samples: -5 }))).toBe(0)
  })
})

describe('examActiveSegment', () => {
  it('walks stability -> region_speed -> unlock -> done', () => {
    expect(examActiveSegment(counts())).toBe('stability')
    expect(examActiveSegment(counts({ samples: 12 }))).toBe('stability')
    // stability finished (metrics arrived) moves on even if samples < 30
    expect(examActiveSegment(counts({ samples: 12, stabilityDone: true }))).toBe('region_speed')
    expect(examActiveSegment(counts({ stabilityDone: true, regions: 3 }))).toBe('region_speed')
    expect(examActiveSegment(counts({ stabilityDone: true, regions: 8 }))).toBe('unlock')
    expect(examActiveSegment(counts({ stabilityDone: true, regions: 8, unlocks: 2 }))).toBe(
      'unlock'
    )
    expect(examActiveSegment(counts({ stabilityDone: true, regions: 8, unlocks: 6 }))).toBe('done')
  })
})

describe('examSegmentCounter', () => {
  it('renders the active segment counter text', () => {
    expect(examSegmentCounter(counts({ samples: 12 })).text).toBe('采样 12/30')
    expect(examSegmentCounter(counts({ stabilityDone: true, regions: 3 })).text).toBe('测速 3/8')
    expect(examSegmentCounter(counts({ stabilityDone: true, regions: 8, unlocks: 4 })).text).toBe(
      '解锁 4/6'
    )
  })

  it('uses the current item name as the label when provided', () => {
    const c = counts({ stabilityDone: true, regions: 3 })
    expect(examSegmentCounter(c, { regionName: '东京' }).text).toBe('东京 3/8')
  })

  it('reports done with full progress', () => {
    const r = examSegmentCounter(counts({ stabilityDone: true, regions: 8, unlocks: 6 }))
    expect(r.segment).toBe('done')
    expect(r.text).toBe('完成')
  })
})
