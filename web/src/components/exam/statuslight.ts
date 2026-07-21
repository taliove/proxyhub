// Status light state machine for exam dialog header.
// Green: normal progress (no errors), Yellow: has errors but not fatal, Red: fatal/connection failure.
import type {
  ExamStabilityMetrics,
  ExamRegionResult,
  ExamUnlockResult,
  ExamEgressMetrics
} from '@/types'
import type { ExamStreamStatus } from './examstream'

export type StatusLight = 'green' | 'yellow' | 'red'

// computeStatusLight determines the status light color based on exam state.
// Red: terminal error states (connection failure, global error)
// Yellow: running/done with any error in region/unlock/egress data
// Green: running/done with no errors
export function computeStatusLight(
  status: ExamStreamStatus | 'idle',
  terminalError: string,
  _stability: ExamStabilityMetrics | null,
  regions: ExamRegionResult[],
  unlocks: ExamUnlockResult[],
  egress: ExamEgressMetrics | null
): StatusLight {
  // Red: connection failure or terminal error
  if (status === 'error' || terminalError) return 'red'

  // Yellow: any error in data rows
  if (hasAnyError(regions, unlocks, egress)) return 'yellow'

  // Green: normal (running/done with no errors)
  return 'green'
}

// hasAnyError checks if any section has error entries.
function hasAnyError(
  regions: ExamRegionResult[],
  unlocks: ExamUnlockResult[],
  egress: ExamEgressMetrics | null
): boolean {
  // Region errors
  if (regions.some((r) => r.error)) return true

  // Unlock errors
  if (unlocks.some((u) => u.error)) return true

  // Egress errors (any of ipv4/ipv6/dns)
  if (egress?.ipv4?.error) return true
  if (egress?.ipv6?.error) return true
  if (egress?.dns?.error) return true

  return false
}
