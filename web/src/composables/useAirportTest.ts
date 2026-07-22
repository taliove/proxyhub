import client from '@/api/client'

export interface DiagnosticResult {
  http_status: number
  duration_ms: number
  node_count: number
  protocol_counts: Record<string, number>
  parse_failures: number
  url_reachable?: boolean // 订阅URL是否可达(HTTP 2xx)
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

// TestRunStatus 测试 run 行状态;cancelled 为任务化(issue 0025)后被显式取消的
// 终态(独立枚举,对齐 jobs.StatusCancelled 与 refresh_runs 口径,ADR 0027)。
export type TestRunStatus =
  'diagnosing' | 'checking' | 'scoring' | 'completed' | 'failed' | 'cancelled'

export interface TestRun {
  id: number
  airport_id: number
  created_at: string
  status: TestRunStatus
  overall_score?: number
  dimensions_json: string
  error_message?: string
  // job_id 关联的 jobs 任务 id(迁入 jobs 运行时后回填;0/缺省 = 任务化前旧记录)
  job_id?: number
}

// AirportTestJobHandle POST /airports/{id}/test 的响应(issue 0025:机场测试迁入
// jobs 运行时,与 /airports/{id}/refresh 同构,返回任务句柄而非 run 行)。
// started=false 表示附加到同机场进行中任务(kind+key 单实例),jobId 为在跑任务 id。
export interface AirportTestJobHandle {
  jobId: number
  kind: string
  key: string
  started: boolean
}

// AirportTestCursor 机场测试任务的 jobs cursor(ADR 0027:主进度源,
// 轮询 /api/jobs/{id} 消费;run 行 sample_params 为镜像,前端不读)。
export interface AirportTestCursor {
  phase: 'diagnosing' | 'checking' | 'scoring'
  checked: number
  total: number
}

/**
 * Run airport test:发起机场测试任务,返回 jobs 任务句柄。
 * 进度轮询 /api/jobs/{jobId} 的 cursor,报告走 /api/jobs/{jobId}/result。
 */
export async function runAirportTest(
  airportId: number,
  full = false
): Promise<AirportTestJobHandle> {
  // 拦截器已解包 response.data(client.post 直接返回数据体)
  return client.post<unknown, AirportTestJobHandle>(`/airports/${airportId}/test`, { full })
}

/**
 * Get a specific test run by ID.
 */
export async function getTestRun(airportId: number, runId: number): Promise<TestRun> {
  return client.get<unknown, TestRun>(`/airports/${airportId}/test/runs/${runId}`)
}

/**
 * List recent test runs for an airport.
 */
export async function listTestRuns(airportId: number): Promise<TestRun[]> {
  return client.get<unknown, TestRun[]>(`/airports/${airportId}/test/runs`)
}

/**
 * emptyDiagnostic 诊断数据空初始值(每次调用返回新对象,不共享引用)。
 */
export function emptyDiagnostic(): DiagnosticResult {
  return {
    http_status: 0,
    duration_ms: 0,
    node_count: 0,
    protocol_counts: {},
    parse_failures: 0
  }
}

/**
 * Parse diagnostic result from dimensions_json string.
 * Pure function for easy testing.
 */
export function parseDiagnosticResult(dimensionsJson: string): DiagnosticResult {
  try {
    return JSON.parse(dimensionsJson) as DiagnosticResult
  } catch {
    return emptyDiagnostic()
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
 * Parse airport test job cursor (jobs cursor JSON: {"phase","checked","total"}).
 * Pure function for easy testing. 非法/空/未知阶段返回 null。
 */
export function parseAirportTestCursor(cursor: string | undefined): AirportTestCursor | null {
  if (!cursor) return null
  try {
    const data = JSON.parse(cursor)
    if (data.phase !== 'diagnosing' && data.phase !== 'checking' && data.phase !== 'scoring') {
      return null
    }
    return {
      phase: data.phase,
      checked: typeof data.checked === 'number' ? data.checked : 0,
      total: typeof data.total === 'number' ? data.total : 0
    }
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

/**
 * Diagnostic state for UI display.
 * Pure function for easy testing.
 */
export type DiagnosticState = 'success' | 'unreachable' | 'failed'

/**
 * Derive diagnostic state from test run status and dimensions.
 * Pure function for easy testing.
 *
 * Three states:
 * - success: URL reachable (HTTP 2xx), test proceeds normally
 * - unreachable: URL unreachable but pool has nodes, test continues with renormalized weights
 * - failed: run failed (pool empty + URL unreachable)
 */
export function getDiagnosticState(
  runStatus: TestRunStatus,
  dimensionsJson: string
): DiagnosticState {
  // Run failed: diagnostic failed
  if (runStatus === 'failed') {
    return 'failed'
  }

  // Parse dimensions to check url_reachable
  try {
    const dims = JSON.parse(dimensionsJson)
    // url_reachable explicitly false means URL unreachable but test continues (pool has nodes)
    if (dims.url_reachable === false) {
      return 'unreachable'
    }
    // url_reachable true or checking/scoring/completed status means success
    if (
      dims.url_reachable === true ||
      runStatus === 'checking' ||
      runStatus === 'scoring' ||
      runStatus === 'completed'
    ) {
      return 'success'
    }
  } catch {
    // Parse error, fall through
  }

  // Diagnosing phase or unknown: default to success
  return 'success'
}
