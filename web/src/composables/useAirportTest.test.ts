import { describe, it, expect } from 'vitest'
import {
  parseDiagnosticResult,
  parseCheckingProgress,
  parseCompletedResult,
  getScoreColor,
  formatDuration,
  isHttpSuccess,
  getDiagnosticState
} from './useAirportTest'

describe('useAirportTest', () => {
  describe('parseDiagnosticResult', () => {
    it('should parse valid JSON', () => {
      const json = JSON.stringify({
        http_status: 200,
        duration_ms: 350,
        node_count: 25,
        protocol_counts: { vmess: 15, ss: 10 },
        parse_failures: 2
      })

      const result = parseDiagnosticResult(json)

      expect(result.http_status).toBe(200)
      expect(result.duration_ms).toBe(350)
      expect(result.node_count).toBe(25)
      expect(result.protocol_counts).toEqual({ vmess: 15, ss: 10 })
      expect(result.parse_failures).toBe(2)
    })

    it('should return default values for invalid JSON', () => {
      const result = parseDiagnosticResult('invalid json')

      expect(result.http_status).toBe(0)
      expect(result.duration_ms).toBe(0)
      expect(result.node_count).toBe(0)
      expect(result.protocol_counts).toEqual({})
      expect(result.parse_failures).toBe(0)
    })

    it('should handle empty string', () => {
      const result = parseDiagnosticResult('')

      expect(result.http_status).toBe(0)
      expect(result.node_count).toBe(0)
    })
  })

  describe('parseCheckingProgress', () => {
    it('should parse valid checking progress', () => {
      const json = JSON.stringify({
        checked: 5,
        total: 10,
        http_status: 200,
        node_count: 10
      })

      const result = parseCheckingProgress(json)

      expect(result).toEqual({ checked: 5, total: 10 })
    })

    it('should return null for missing fields', () => {
      const json = JSON.stringify({ http_status: 200 })

      const result = parseCheckingProgress(json)

      expect(result).toBeNull()
    })

    it('should return null for invalid JSON', () => {
      const result = parseCheckingProgress('invalid')

      expect(result).toBeNull()
    })
  })

  describe('parseCompletedResult', () => {
    it('should parse valid completed result', () => {
      const json = JSON.stringify({
        http_status: 200,
        node_count: 10,
        checked: 10,
        total: 10,
        availability_rate: 0.9,
        latency_mean_ms: 120,
        latency_p95_ms: 200,
        score_breakdown: {
          availability_rate: 0.9,
          availability_score: 45,
          latency_mean_ms: 120,
          latency_p95_ms: 200,
          latency_score: 28,
          fetch_health_score: 10,
          region_coverage_count: 3,
          region_coverage_score: 7.5
        }
      })

      const result = parseCompletedResult(json)

      expect(result).not.toBeNull()
      expect(result?.score_breakdown.availability_score).toBe(45)
      expect(result?.score_breakdown.latency_score).toBe(28)
    })

    it('should return null for diagnostic-only result', () => {
      const json = JSON.stringify({
        http_status: 200,
        node_count: 10
      })

      const result = parseCompletedResult(json)

      expect(result).toBeNull()
    })

    it('should return null for invalid JSON', () => {
      const result = parseCompletedResult('invalid')

      expect(result).toBeNull()
    })
  })

  describe('getScoreColor', () => {
    it('should return success for scores >= 80', () => {
      expect(getScoreColor(80)).toBe('success')
      expect(getScoreColor(90)).toBe('success')
      expect(getScoreColor(100)).toBe('success')
    })

    it('should return warning for scores >= 60 and < 80', () => {
      expect(getScoreColor(60)).toBe('warning')
      expect(getScoreColor(70)).toBe('warning')
      expect(getScoreColor(79)).toBe('warning')
    })

    it('should return danger for scores < 60', () => {
      expect(getScoreColor(0)).toBe('danger')
      expect(getScoreColor(30)).toBe('danger')
      expect(getScoreColor(59)).toBe('danger')
    })
  })

  describe('formatDuration', () => {
    it('should format milliseconds for values < 1000', () => {
      expect(formatDuration(350)).toBe('350 ms')
      expect(formatDuration(0)).toBe('0 ms')
      expect(formatDuration(999)).toBe('999 ms')
    })

    it('should format seconds for values >= 1000', () => {
      expect(formatDuration(1000)).toBe('1.00 s')
      expect(formatDuration(1500)).toBe('1.50 s')
      expect(formatDuration(5234)).toBe('5.23 s')
    })
  })

  describe('isHttpSuccess', () => {
    it('should return true for 2xx status codes', () => {
      expect(isHttpSuccess(200)).toBe(true)
      expect(isHttpSuccess(201)).toBe(true)
      expect(isHttpSuccess(204)).toBe(true)
      expect(isHttpSuccess(299)).toBe(true)
    })

    it('should return false for non-2xx status codes', () => {
      expect(isHttpSuccess(199)).toBe(false)
      expect(isHttpSuccess(300)).toBe(false)
      expect(isHttpSuccess(404)).toBe(false)
      expect(isHttpSuccess(500)).toBe(false)
      expect(isHttpSuccess(0)).toBe(false)
    })
  })

  describe('getDiagnosticState', () => {
    it('should return success when URL is reachable (url_reachable=true)', () => {
      const dimensionsJson = JSON.stringify({ url_reachable: true, http_status: 200 })
      expect(getDiagnosticState('checking', dimensionsJson)).toBe('success')
      expect(getDiagnosticState('scoring', dimensionsJson)).toBe('success')
      expect(getDiagnosticState('completed', dimensionsJson)).toBe('success')
    })

    it('should return unreachable when URL is not reachable but test continues (url_reachable=false)', () => {
      const dimensionsJson = JSON.stringify({ url_reachable: false, http_status: 404 })
      expect(getDiagnosticState('checking', dimensionsJson)).toBe('unreachable')
      expect(getDiagnosticState('scoring', dimensionsJson)).toBe('unreachable')
      expect(getDiagnosticState('completed', dimensionsJson)).toBe('unreachable')
    })

    it('should return failed when run status is failed', () => {
      const dimensionsJson = JSON.stringify({ url_reachable: false, http_status: 404 })
      expect(getDiagnosticState('failed', dimensionsJson)).toBe('failed')
    })

    it('should return success for diagnosing phase (before url_reachable is known)', () => {
      const dimensionsJson = JSON.stringify({ http_status: 0 })
      expect(getDiagnosticState('diagnosing', dimensionsJson)).toBe('success')
    })

    it('should return success when dimensions cannot be parsed', () => {
      const invalidJson = 'invalid json'
      expect(getDiagnosticState('checking', invalidJson)).toBe('success')
    })

    it('should return success when url_reachable is missing but status is checking/scoring/completed', () => {
      const dimensionsJson = JSON.stringify({ http_status: 200 })
      expect(getDiagnosticState('checking', dimensionsJson)).toBe('success')
      expect(getDiagnosticState('scoring', dimensionsJson)).toBe('success')
      expect(getDiagnosticState('completed', dimensionsJson)).toBe('success')
    })
  })
})
