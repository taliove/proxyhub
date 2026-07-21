import { describe, it, expect } from 'vitest'
import { computeSparklineYAxis, computeSparklineXAxis } from './sparklineaxes'
import type { ExamStabilitySample } from '@/types'

const sample = (seq: number, latency: number, ok = true): ExamStabilitySample => ({
  seq,
  elapsed_ms: seq * 1000,
  latency_ms: latency,
  ok
})

describe('computeSparklineYAxis', () => {
  it('returns 3-4 ticks covering min to max latency with nice round numbers', () => {
    const samples = [sample(0, 50), sample(1, 100), sample(2, 150)]
    const ticks = computeSparklineYAxis(samples, 56)
    expect(ticks.length).toBeGreaterThanOrEqual(3)
    expect(ticks.length).toBeLessThanOrEqual(4)
    expect(ticks[0].value).toBeLessThanOrEqual(50) // covers min
    expect(ticks[ticks.length - 1].value).toBeGreaterThanOrEqual(150) // covers max
  })

  it('uses nice step intervals (10/20/25/50/100)', () => {
    const samples = [sample(0, 10), sample(1, 90)]
    const ticks = computeSparklineYAxis(samples, 56)
    const steps = ticks.slice(1).map((t, i) => t.value - ticks[i].value)
    const allNice = steps.every((s) => [10, 20, 25, 50, 100, 200, 250, 500].includes(s))
    expect(allNice).toBe(true)
  })

  it('positions ticks correctly in viewBox height', () => {
    const samples = [sample(0, 0), sample(1, 100)]
    const ticks = computeSparklineYAxis(samples, 56)
    expect(ticks[0].y).toBe(56) // max latency at top (y=0 in viewBox, but we flip)
    expect(ticks[ticks.length - 1].y).toBe(0) // min latency at bottom
  })

  it('handles samples with all same latency', () => {
    const samples = [sample(0, 50), sample(1, 50), sample(2, 50)]
    const ticks = computeSparklineYAxis(samples, 56)
    expect(ticks.length).toBeGreaterThanOrEqual(2)
    expect(ticks.some((t) => t.value === 50)).toBe(true)
  })

  it('returns empty for no successful samples', () => {
    expect(computeSparklineYAxis([], 56)).toEqual([])
    expect(computeSparklineYAxis([sample(0, 50, false)], 56)).toEqual([])
  })

  it('formats tick labels as integer ms', () => {
    const samples = [sample(0, 10), sample(1, 90)]
    const ticks = computeSparklineYAxis(samples, 56)
    ticks.forEach((t) => {
      expect(t.label).toMatch(/^\d+ ms$/)
    })
  })
})

describe('computeSparklineXAxis', () => {
  it('returns ticks at start/middle/end of sample range', () => {
    const samples = [sample(0, 50), sample(5, 60), sample(10, 70)]
    const ticks = computeSparklineXAxis(samples, 300)
    expect(ticks.length).toBe(3)
    expect(ticks[0].x).toBe(0)
    expect(ticks[1].x).toBeCloseTo(150, 0) // middle
    expect(ticks[2].x).toBe(300)
  })

  it('labels ticks with elapsed seconds', () => {
    const samples = [sample(0, 50), sample(5, 60), sample(10, 70)]
    const ticks = computeSparklineXAxis(samples, 300)
    expect(ticks[0].label).toBe('0s')
    expect(ticks[1].label).toBe('5s')
    expect(ticks[2].label).toBe('10s')
  })

  it('returns empty for insufficient samples', () => {
    expect(computeSparklineXAxis([], 300)).toEqual([])
    expect(computeSparklineXAxis([sample(0, 50)], 300)).toEqual([])
  })

  it('handles only failed samples', () => {
    const samples = [sample(0, 50, false), sample(1, 60, false)]
    expect(computeSparklineXAxis(samples, 300)).toEqual([])
  })
})
