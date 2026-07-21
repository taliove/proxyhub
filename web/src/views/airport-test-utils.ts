import { relativeTimeZh, parseTimeMs } from '@/components/exam/examhistory'

// Score color thresholds for airport test results
export type ScoreColor = 'success' | 'warning' | 'danger' | 'info'

export function scoreColor(score: number | null | undefined, status?: string | null): ScoreColor {
  if (score === null || score === undefined) {
    return status === 'failed' ? 'info' : 'info'
  }
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
}

// Format airport test time as relative time (reusing exam history logic)
export function testTimeRelative(isoTime: string | null | undefined): string {
  if (!isoTime) return '-'
  return relativeTimeZh(parseTimeMs(isoTime))
}

// Format score display with precision
export function scoreDisplay(score: number | null | undefined): string {
  if (score === null || score === undefined) return '-'
  return score.toFixed(1)
}
