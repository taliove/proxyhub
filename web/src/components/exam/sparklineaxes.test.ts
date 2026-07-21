import { describe, it, expect } from 'vitest'
import {
  computeSparklineYAxis,
  computeSparklineXAxis,
  computeSparklineLayout
} from './sparklineaxes'
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

describe('computeSparklineLayout', () => {
  it('calculates gutter width from max Y-axis label', () => {
    const samples = [sample(0, 999), sample(1, 1000)]
    const layout = computeSparklineLayout(samples, 300, 56)
    // "1000 ms" is longest label, should need ~35-40px gutter
    expect(layout.gutterLeft).toBeGreaterThan(30)
    expect(layout.gutterLeft).toBeLessThan(50)
    expect(layout.plotAreaOffsetX).toBe(layout.gutterLeft)
  })

  it('no samples → zero gutter', () => {
    const layout = computeSparklineLayout([], 300, 56)
    expect(layout.gutterLeft).toBe(0)
    expect(layout.plotAreaOffsetX).toBe(0)
    expect(layout.plotAreaOffsetY).toBe(0)
  })

  it('reserves top padding for Y-axis labels', () => {
    const samples = [sample(0, 50), sample(1, 100)]
    const layout = computeSparklineLayout(samples, 300, 56)
    // Should have small top padding for label half-height
    expect(layout.plotAreaOffsetY).toBeGreaterThan(0)
    expect(layout.plotAreaOffsetY).toBeLessThan(10)
  })

  it('returns consistent structure', () => {
    const samples = [sample(0, 50)]
    const layout = computeSparklineLayout(samples, 300, 56)
    expect(layout).toHaveProperty('gutterLeft')
    expect(layout).toHaveProperty('plotAreaOffsetX')
    expect(layout).toHaveProperty('plotAreaOffsetY')
    expect(typeof layout.gutterLeft).toBe('number')
    expect(typeof layout.plotAreaOffsetX).toBe('number')
    expect(typeof layout.plotAreaOffsetY).toBe('number')
  })

  it('small latency values need smaller gutter', () => {
    const samples = [sample(0, 5), sample(1, 15)]
    const layout = computeSparklineLayout(samples, 300, 56)
    // "20 ms" or similar short label
    expect(layout.gutterLeft).toBeGreaterThan(15)
    expect(layout.gutterLeft).toBeLessThan(35)
  })

  it('reserves a bottom gutter for X-axis labels + lowest Y label half', () => {
    const samples = [sample(0, 50), sample(1, 100)]
    const layout = computeSparklineLayout(samples, 300, 56)
    // Bottom gutter holds one 9px label line plus half the Y label height.
    expect(layout.gutterBottom).toBeGreaterThan(9)
    expect(layout.gutterBottom).toBeLessThan(20)
  })

  it('no samples → zero bottom gutter', () => {
    const layout = computeSparklineLayout([], 300, 56)
    expect(layout.gutterBottom).toBe(0)
  })
})

describe('axis clipping — all tick labels stay inside the viewBox', () => {
  const VB_H = 56
  const VB_W = 300

  it('lowest Y tick (e.g. "0 ms") sits above the viewBox bottom by its label half-height', () => {
    const samples = [sample(0, 0), sample(1, 100)]
    const layout = computeSparklineLayout(samples, VB_W, VB_H)
    const ticks = computeSparklineYAxis(samples, VB_H, layout.plotAreaOffsetY, layout.gutterBottom)
    // Lowest tick is the max y (bottom-most). It must clear the viewBox floor by >= half a label,
    // so a middle-baseline label is not clipped at the bottom.
    const lowest = Math.max(...ticks.map((t) => t.y))
    expect(lowest).toBeLessThanOrEqual(VB_H - 4) // ~half of 9px font
    // And the "0 ms" label is actually present and drawn inside the box.
    const zero = ticks.find((t) => t.value === 0)
    expect(zero).toBeDefined()
    expect(zero!.y).toBeLessThanOrEqual(VB_H)
  })

  it('highest Y tick has room above for its upper half (>= top padding)', () => {
    const samples = [sample(0, 0), sample(1, 100)]
    const layout = computeSparklineLayout(samples, VB_W, VB_H)
    const ticks = computeSparklineYAxis(samples, VB_H, layout.plotAreaOffsetY, layout.gutterBottom)
    const highest = Math.min(...ticks.map((t) => t.y))
    expect(highest).toBeGreaterThanOrEqual(layout.plotAreaOffsetY - 0.01)
    expect(highest).toBeGreaterThanOrEqual(4) // upper half not clipped at top
  })

  it('X-axis labels hang inside the bottom gutter, not past the viewBox floor', () => {
    const samples = [sample(0, 50), sample(5, 60), sample(10, 70)]
    const layout = computeSparklineLayout(samples, VB_W, VB_H)
    const plotBottom = VB_H - layout.gutterBottom
    const xTicks = computeSparklineXAxis(samples, VB_W, layout.plotAreaOffsetX, plotBottom + 1)
    xTicks.forEach((t) => {
      expect(t.y).toBeGreaterThanOrEqual(plotBottom) // below the plot
      // hanging baseline + one line of text must fit before the floor
      expect(t.y + 9).toBeLessThanOrEqual(VB_H + 0.01)
    })
  })

  it('same-latency degenerate case: bottom tick still clears the floor', () => {
    const samples = [sample(0, 50), sample(1, 50), sample(2, 50)]
    const layout = computeSparklineLayout(samples, VB_W, VB_H)
    const ticks = computeSparklineYAxis(samples, VB_H, layout.plotAreaOffsetY, layout.gutterBottom)
    const lowest = Math.max(...ticks.map((t) => t.y))
    expect(lowest).toBeLessThanOrEqual(VB_H - 4)
  })

  it('X-axis ticks carry a y coordinate', () => {
    const samples = [sample(0, 50), sample(5, 60), sample(10, 70)]
    const ticks = computeSparklineXAxis(samples, VB_W, 0, 44)
    ticks.forEach((t) => expect(t.y).toBe(44))
  })
})
