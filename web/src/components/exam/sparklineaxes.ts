// Sparkline axis computation for stability section.
// Y-axis: latency scale with nice round ticks (adaptive to data range)
// X-axis: time scale showing elapsed seconds at start/middle/end
// Layout: computes gutter space for Y-axis labels to prevent overlap with plot area
import type { ExamStabilitySample } from '@/types'

export interface YAxisTick {
  value: number // latency in ms
  y: number // y coordinate in viewBox
  label: string // formatted label (e.g., "50 ms")
}

export interface XAxisTick {
  value: number // elapsed time in seconds
  x: number // x coordinate in viewBox
  label: string // formatted label (e.g., "5s")
}

export interface SparklineLayout {
  gutterLeft: number // left gutter width for Y-axis labels (px in viewBox)
  plotAreaOffsetX: number // plot area starts at this X offset
  plotAreaOffsetY: number // plot area starts at this Y offset (top padding)
}

// computeSparklineYAxis generates 3-4 ticks for the Y-axis covering the latency range.
// Uses nice intervals (10/20/25/50/100/...) and ensures min/max are covered.
// plotAreaOffsetY shifts the plot area down from the top to avoid label overflow.
export function computeSparklineYAxis(
  samples: ExamStabilitySample[],
  viewBoxHeight: number,
  plotAreaOffsetY = 0
): YAxisTick[] {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length === 0) return []

  const min = Math.min(...ok.map((s) => s.latency_ms))
  const max = Math.max(...ok.map((s) => s.latency_ms))

  const plotHeight = viewBoxHeight - plotAreaOffsetY

  // If all samples have same latency, show single range
  if (min === max) {
    const base = Math.floor(min / 10) * 10
    const ceil = base + 20
    return [
      { value: base, y: viewBoxHeight, label: `${base} ms` },
      { value: min, y: plotAreaOffsetY + plotHeight / 2, label: `${Math.round(min)} ms` },
      { value: ceil, y: plotAreaOffsetY, label: `${ceil} ms` }
    ]
  }

  const range = max - min
  const niceStep = computeNiceStep(range)
  const minTick = Math.floor(min / niceStep) * niceStep
  const maxTick = Math.ceil(max / niceStep) * niceStep

  const ticks: YAxisTick[] = []
  for (let v = minTick; v <= maxTick; v += niceStep) {
    const y = plotAreaOffsetY + plotHeight - ((v - minTick) / (maxTick - minTick)) * plotHeight
    ticks.push({ value: v, y: round2(y), label: `${Math.round(v)} ms` })
  }

  return ticks
}

// computeSparklineXAxis generates 3 ticks (start/middle/end) showing elapsed seconds.
// plotAreaOffsetX shifts the plot area right to make room for Y-axis labels.
export function computeSparklineXAxis(
  samples: ExamStabilitySample[],
  viewBoxWidth: number,
  plotAreaOffsetX = 0
): XAxisTick[] {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length < 2) return []

  const startSec = Math.round(ok[0].elapsed_ms / 1000)
  const midSec = Math.round(ok[Math.floor(ok.length / 2)].elapsed_ms / 1000)
  const endSec = Math.round(ok[ok.length - 1].elapsed_ms / 1000)

  const plotWidth = viewBoxWidth - plotAreaOffsetX
  const stepX = ok.length > 1 ? plotWidth / (ok.length - 1) : 0

  return [
    { value: startSec, x: plotAreaOffsetX, label: `${startSec}s` },
    {
      value: midSec,
      x: round2(plotAreaOffsetX + Math.floor(ok.length / 2) * stepX),
      label: `${midSec}s`
    },
    { value: endSec, x: viewBoxWidth, label: `${endSec}s` }
  ]
}

// computeSparklineLayout calculates gutter and offsets for Y-axis labels.
// Returns left gutter width and plot area offsets to prevent label-curve overlap.
export function computeSparklineLayout(
  samples: ExamStabilitySample[],
  _viewBoxWidth: number,
  viewBoxHeight: number
): SparklineLayout {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length === 0) {
    return { gutterLeft: 0, plotAreaOffsetX: 0, plotAreaOffsetY: 0 }
  }

  // Compute Y-axis ticks with zero offset to get label text
  const tempTicks = computeSparklineYAxis(samples, viewBoxHeight, 0)
  if (tempTicks.length === 0) {
    return { gutterLeft: 0, plotAreaOffsetX: 0, plotAreaOffsetY: 0 }
  }

  // Find longest label (assumes monospace-like metrics for "N ms" format)
  const maxLabelLength = Math.max(...tempTicks.map((t) => t.label.length))

  // Estimate width: ~5.5px per character at 9px font size + 4px padding
  const gutterLeft = Math.ceil(maxLabelLength * 5.5 + 4)

  // Top padding: half the font height (~4.5px for 9px font) to prevent top label overflow
  const plotAreaOffsetY = 5

  return {
    gutterLeft,
    plotAreaOffsetX: gutterLeft,
    plotAreaOffsetY
  }
}

// computeNiceStep picks a round step size for 3-4 ticks covering the range.
function computeNiceStep(range: number): number {
  const raw = range / 3
  const magnitude = Math.pow(10, Math.floor(Math.log10(raw)))
  const normalized = raw / magnitude

  let niceNorm: number
  if (normalized <= 1) niceNorm = 1
  else if (normalized <= 2) niceNorm = 2
  else if (normalized <= 2.5) niceNorm = 2.5
  else if (normalized <= 5) niceNorm = 5
  else niceNorm = 10

  return niceNorm * magnitude
}

function round2(v: number): number {
  return Math.round(v * 100) / 100
}
