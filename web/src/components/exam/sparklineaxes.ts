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
  y: number // y coordinate for the label baseline in viewBox
  label: string // formatted label (e.g., "5s")
}

export interface SparklineLayout {
  gutterLeft: number // left gutter width for Y-axis labels (px in viewBox)
  gutterBottom: number // bottom gutter height for X-axis labels + lowest Y label half (px)
  plotAreaOffsetX: number // plot area starts at this X offset
  plotAreaOffsetY: number // plot area starts at this Y offset (top padding)
}

// computeSparklineYAxis generates 3-4 ticks for the Y-axis covering the latency range.
// Uses nice intervals (10/20/25/50/100/...) and ensures min/max are covered.
// plotAreaOffsetY reserves top padding; plotAreaOffsetBottom reserves bottom padding so the
// lowest tick (e.g. "0 ms") and the X-axis labels stay fully inside the viewBox (no clipping).
export function computeSparklineYAxis(
  samples: ExamStabilitySample[],
  viewBoxHeight: number,
  plotAreaOffsetY = 0,
  plotAreaOffsetBottom = 0
): YAxisTick[] {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length === 0) return []

  const min = Math.min(...ok.map((s) => s.latency_ms))
  const max = Math.max(...ok.map((s) => s.latency_ms))

  // Plot spans [plotAreaOffsetY, viewBoxHeight - plotAreaOffsetBottom]; bottom tick sits at
  // plotBottom (not the raw viewBox edge) so its label's lower half is not clipped.
  const plotBottom = viewBoxHeight - plotAreaOffsetBottom
  const plotHeight = plotBottom - plotAreaOffsetY

  // If all samples have same latency, show single range
  if (min === max) {
    const base = Math.floor(min / 10) * 10
    const ceil = base + 20
    return [
      { value: base, y: round2(plotBottom), label: `${base} ms` },
      { value: min, y: round2(plotAreaOffsetY + plotHeight / 2), label: `${Math.round(min)} ms` },
      { value: ceil, y: round2(plotAreaOffsetY), label: `${ceil} ms` }
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

// Axis label font metrics (kept in one place; SVG font-size px == viewBox user units here).
const AXIS_FONT_PX = 9
const AXIS_LABEL_HALF = AXIS_FONT_PX / 2 // half text height, for middle-baseline top/bottom overflow
const AXIS_CHAR_WIDTH = 5.5 // approx glyph advance at 9px

// computeSparklineXAxis generates 3 ticks (start/middle/end) showing elapsed seconds.
// plotAreaOffsetX shifts the plot area right to make room for Y-axis labels; labelY places the
// (hanging) label baseline — callers pass the top of the bottom gutter so labels stay in-viewBox.
export function computeSparklineXAxis(
  samples: ExamStabilitySample[],
  viewBoxWidth: number,
  plotAreaOffsetX = 0,
  labelY = 0
): XAxisTick[] {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length < 2) return []

  const startSec = Math.round(ok[0].elapsed_ms / 1000)
  const midSec = Math.round(ok[Math.floor(ok.length / 2)].elapsed_ms / 1000)
  const endSec = Math.round(ok[ok.length - 1].elapsed_ms / 1000)

  const plotWidth = viewBoxWidth - plotAreaOffsetX
  const stepX = ok.length > 1 ? plotWidth / (ok.length - 1) : 0

  return [
    { value: startSec, x: plotAreaOffsetX, y: round2(labelY), label: `${startSec}s` },
    {
      value: midSec,
      x: round2(plotAreaOffsetX + Math.floor(ok.length / 2) * stepX),
      y: round2(labelY),
      label: `${midSec}s`
    },
    { value: endSec, x: viewBoxWidth, y: round2(labelY), label: `${endSec}s` }
  ]
}

// computeSparklineLayout calculates gutters and plot-area offsets so no axis label is clipped by
// the viewBox: left gutter for Y labels, top padding for the highest Y label's upper half, and a
// bottom gutter holding the X-axis label line plus the lowest Y label's lower half.
export function computeSparklineLayout(
  samples: ExamStabilitySample[],
  _viewBoxWidth: number,
  viewBoxHeight: number
): SparklineLayout {
  const ok = samples.filter((s) => s.ok && Number.isFinite(s.latency_ms))
  if (ok.length === 0) {
    return { gutterLeft: 0, gutterBottom: 0, plotAreaOffsetX: 0, plotAreaOffsetY: 0 }
  }

  // Compute Y-axis ticks with zero offsets to get label text
  const tempTicks = computeSparklineYAxis(samples, viewBoxHeight, 0, 0)
  if (tempTicks.length === 0) {
    return { gutterLeft: 0, gutterBottom: 0, plotAreaOffsetX: 0, plotAreaOffsetY: 0 }
  }

  // Find longest label (assumes monospace-like metrics for "N ms" format)
  const maxLabelLength = Math.max(...tempTicks.map((t) => t.label.length))

  // Estimate width: ~5.5px per glyph at 9px font size + 4px padding
  const gutterLeft = Math.ceil(maxLabelLength * AXIS_CHAR_WIDTH + 4)

  // Top padding: half the font height so the highest tick's upper half is not clipped.
  const plotAreaOffsetY = Math.ceil(AXIS_LABEL_HALF)

  // Bottom gutter: the lowest Y tick sits at the plot bottom (middle-baseline, needs half the
  // font height below it) and the X-axis label line hangs beneath that. Reserve both + small gap.
  const gutterBottom = Math.ceil(AXIS_LABEL_HALF + AXIS_FONT_PX + 2)

  return {
    gutterLeft,
    gutterBottom,
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
