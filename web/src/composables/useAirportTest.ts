import client from '@/api/client'

export interface DiagnosticResult {
  http_status: number
  duration_ms: number
  node_count: number
  protocol_counts: Record<string, number>
  parse_failures: number
}

export interface TestRun {
  id: number
  airport_id: number
  created_at: string
  status: 'diagnosing' | 'completed' | 'failed'
  dimensions_json: string
  error_message?: string
}

/**
 * Run airport test and return the test run result.
 */
export async function runAirportTest(airportId: number, full = false): Promise<TestRun> {
  const response = await client.post<unknown, TestRun>(`/airports/${airportId}/test`, { full })
  return response
}

/**
 * Parse diagnostic result from dimensions_json string.
 * Pure function for easy testing.
 */
export function parseDiagnosticResult(dimensionsJson: string): DiagnosticResult {
  try {
    return JSON.parse(dimensionsJson) as DiagnosticResult
  } catch {
    return {
      http_status: 0,
      duration_ms: 0,
      node_count: 0,
      protocol_counts: {},
      parse_failures: 0
    }
  }
}

/**
 * Format duration for display.
 * Pure function for easy testing.
 */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

/**
 * Check if HTTP status is success.
 * Pure function for easy testing.
 */
export function isHttpSuccess(status: number): boolean {
  return status >= 200 && status < 300
}
