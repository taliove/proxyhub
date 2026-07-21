import client from '@/api/client'

export interface DiagnosticResult {
  http_status: number
  duration_ms: number
  node_count: number
  protocol_counts: Record<string, number>
  parse_failures: number
}

export interface CheckingProgress {
  checked: number
  total: number
}

export interface ScoreBreakdown {
  availability_rate: number
  availability_score: number
  latency_mean_ms: number
  latency_p95_ms: number
  latency_score: number
  fetch_health_score: number
  region_coverage_count: number
  region_coverage_score: number
}

export interface CompletedResult extends DiagnosticResult {
  checked: number
  total: number
  availability_rate: number
  latency_mean_ms: number
  latency_p95_ms: number
  fetch_duration_ms: number
  region_coverage_count: number
  score_breakdown: ScoreBreakdown
}

export type TestRunStatus = 'diagnosing' | 'checking' | 'scoring' | 'completed' | 'failed'

export interface TestRun {
  id: number
  airport_id: number
  created_at: string
  status: TestRunStatus
  overall_score?: number
  dimensions_json: string
  error_message?: string
}

/**
 * Run airport test and return the test run result.
 */
export async function runAirportTest(airportId: number, full = false): Promise<TestRun> {
  const response = await client.post<TestRun>(`/airports/${airportId}/test`, { full })
  return response.data
}

/**
 * Get a specific test run by ID.
 */
export async function getTestRun(airportId: number, runId: number): Promise<TestRun> {
  const response = await client.get<TestRun>(`/airports/${airportId}/test/runs/${runId}`)
  return response.data
}

/**
 * List recent test runs for an airport.
 */
export async function listTestRuns(airportId: number): Promise<TestRun[]> {
  const response = await client.get<TestRun[]>(`/airports/${airportId}/test/runs`)
  return response.data
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
 * Parse checking progress from dimensions_json (checking phase).
 * Pure function for easy testing.
 */
export function parseCheckingProgress(dimensionsJson: string): CheckingProgress | null {
  try {
    const data = JSON.parse(dimensionsJson)
    if (typeof data.checked === 'number' && typeof data.total === 'number') {
      return { checked: data.checked, total: data.total }
    }
    return null
  } catch {
    return null
  }
}

/**
 * Parse completed result from dimensions_json (completed phase).
 * Pure function for easy testing.
 */
export function parseCompletedResult(dimensionsJson: string): CompletedResult | null {
  try {
    const data = JSON.parse(dimensionsJson)
    if (data.score_breakdown) {
      return data as CompletedResult
    }
    return null
  } catch {
    return null
  }
}

/**
 * Get score color based on score value.
 * Pure function for easy testing.
 */
export function getScoreColor(score: number): 'success' | 'warning' | 'danger' {
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
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
