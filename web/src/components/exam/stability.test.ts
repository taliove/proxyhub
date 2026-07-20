import { describe, it, expect } from 'vitest'
import {
  scoreLevel,
  scoreLabel,
  scoreColorVar,
  formatMs,
  formatLossPct,
  buildSparklinePoints,
  buildSparklinePath
} from './stability'
import type { ExamStabilitySample } from '@/types'

const sample = (latency: number, ok = true, seq = 0): ExamStabilitySample => ({
  seq,
  elapsed_ms: seq * 1000,
  latency_ms: latency,
  ok
})

describe('scoreLevel / label / color', () => {
  it('maps score to three levels at the 85/60 thresholds', () => {
    expect(scoreLevel(85)).toBe('good')
    expect(scoreLevel(84)).toBe('fair')
    expect(scoreLevel(60)).toBe('fair')
    expect(scoreLevel(59)).toBe('poor')
    expect(scoreLevel(0)).toBe('poor')
  })

  it('labels and color vars follow the level', () => {
    expect(scoreLabel(90)).toBe('稳定')
    expect(scoreLabel(70)).toBe('一般')
    expect(scoreLabel(10)).toBe('不稳定')
    expect(scoreColorVar(90)).toBe('--ph-success')
    expect(scoreColorVar(70)).toBe('--ph-warning')
    expect(scoreColorVar(10)).toBe('--ph-danger')
  })
})

describe('formatMs', () => {
  it('rounds and appends unit', () => {
    expect(formatMs(49.6)).toBe('50 ms')
    expect(formatMs(0)).toBe('0 ms')
  })
  it('shows placeholder for undefined / non-finite', () => {
    expect(formatMs(undefined)).toBe('-')
    expect(formatMs(Number.NaN)).toBe('-')
  })
})

describe('formatLossPct', () => {
  it('converts 0..1 to percent', () => {
    expect(formatLossPct(0)).toBe('0%')
    expect(formatLossPct(1)).toBe('100%')
    expect(formatLossPct(0.25)).toBe('25%')
    expect(formatLossPct(0.123)).toBe('12.3%')
  })
  it('clamps out-of-range and handles undefined', () => {
    expect(formatLossPct(1.5)).toBe('100%')
    expect(formatLossPct(undefined)).toBe('-')
  })
})

describe('buildSparklinePoints', () => {
  it('returns empty for no successful samples', () => {
    expect(buildSparklinePoints([], 100, 40)).toEqual([])
    expect(buildSparklinePoints([sample(50, false)], 100, 40)).toEqual([])
  })

  it('skips loss points and only plots successful ones', () => {
    const pts = buildSparklinePoints(
      [sample(50, true, 0), sample(0, false, 1), sample(100, true, 2)],
      100,
      40
    )
    expect(pts).toHaveLength(2)
  })

  it('maps the max latency to the top (y=0) and spreads x across width', () => {
    const pts = buildSparklinePoints([sample(50, true, 0), sample(100, true, 1)], 100, 40)
    // 两点:x 端点在 0 和 width;最大延迟(100)映射到 y=0,较小的 50 映射到 height/2。
    expect(pts[0]).toEqual({ x: 0, y: 20 })
    expect(pts[1]).toEqual({ x: 100, y: 0 })
  })

  it('centers a single point', () => {
    const pts = buildSparklinePoints([sample(50, true)], 100, 40)
    expect(pts).toHaveLength(1)
    expect(pts[0].x).toBe(50)
    expect(pts[0].y).toBe(0)
  })
})

describe('buildSparklinePath', () => {
  it('joins points into an SVG polyline string', () => {
    expect(
      buildSparklinePath([
        { x: 0, y: 20 },
        { x: 100, y: 0 }
      ])
    ).toBe('0,20 100,0')
    expect(buildSparklinePath([])).toBe('')
  })
})
