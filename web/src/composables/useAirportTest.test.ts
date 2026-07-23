import { describe, it, expect } from 'vitest'
import {
  parseDiagnosticResult,
  parseCheckingProgress,
  parseCompletedResult,
  parseAirportTestCursor,
  getScoreColor,
  formatDuration,
  isHttpSuccess,
  getDiagnosticState,
  dimensionWeights,
  dimensionWeightsOf,
  weightLabel
} from './useAirportTest'

describe('useAirportTest', () => {
  // 与后端 airporttest.ScoreDimensions 对齐的扁平结构(completed run 的 dimensions_json)
  const completedDims = {
    availability_score: 45,
    latency_score: 28,
    fetch_health_score: 10,
    region_score: 7.5,
    available_nodes: 9,
    total_nodes: 10,
    mean_latency_ms: 120,
    p95_latency_ms: 200,
    region_count: 3,
    region_distribution: { HK: 4, SG: 3, US: 3 },
    http_status: 200,
    parse_success_rate: 1,
    url_reachable: true
  }

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

  describe('parseAirportTestCursor', () => {
    it('parses diagnosing cursor (no progress numbers)', () => {
      expect(parseAirportTestCursor('{"phase":"diagnosing","checked":0,"total":0}')).toEqual({
        phase: 'diagnosing',
        checked: 0,
        total: 0
      })
    })

    it('parses checking cursor with progress', () => {
      expect(parseAirportTestCursor('{"phase":"checking","checked":3,"total":12}')).toEqual({
        phase: 'checking',
        checked: 3,
        total: 12
      })
    })

    it('parses scoring cursor', () => {
      expect(parseAirportTestCursor('{"phase":"scoring","checked":0,"total":0}')?.phase).toBe(
        'scoring'
      )
    })

    it('tolerates missing checked/total', () => {
      expect(parseAirportTestCursor('{"phase":"checking"}')).toEqual({
        phase: 'checking',
        checked: 0,
        total: 0
      })
    })

    it('returns null for empty/invalid/unknown phase', () => {
      expect(parseAirportTestCursor(undefined)).toBeNull()
      expect(parseAirportTestCursor('')).toBeNull()
      expect(parseAirportTestCursor('not-json')).toBeNull()
      expect(parseAirportTestCursor('7')).toBeNull()
      expect(parseAirportTestCursor('{"phase":"completed","checked":1,"total":1}')).toBeNull()
    })
  })

  describe('parseCompletedResult', () => {
    it('should parse valid completed result', () => {
      const result = parseCompletedResult(JSON.stringify(completedDims))

      expect(result).not.toBeNull()
      expect(result?.availability_score).toBe(45)
      expect(result?.latency_score).toBe(28)
      expect(result?.available_nodes).toBe(9)
      expect(result?.total_nodes).toBe(10)
      expect(result?.url_reachable).toBe(true)
    })

    it('should parse sampled node details when present', () => {
      const json = JSON.stringify({
        ...completedDims,
        sampled_nodes: [
          { name: '🇭🇰 香港 TA-01', region: 'HK', available: true, latency_ms: 120 },
          { name: 'XX 新加坡 01', region: 'SG', available: false, latency_ms: 0, error: 'timeout' }
        ]
      })

      const result = parseCompletedResult(json)

      expect(result?.sampled_nodes).toHaveLength(2)
      expect(result?.sampled_nodes?.[0].name).toBe('🇭🇰 香港 TA-01')
      expect(result?.sampled_nodes?.[1].available).toBe(false)
    })

    it('should leave sampled_nodes undefined for old runs without details', () => {
      const result = parseCompletedResult(JSON.stringify(completedDims))

      expect(result).not.toBeNull()
      expect(result?.sampled_nodes).toBeUndefined()
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

  describe('dimensionWeights', () => {
    it('should return normal weights when URL reachable (50/30/10/10)', () => {
      expect(dimensionWeights(true)).toEqual({
        availability: 50,
        latency: 30,
        fetchHealth: 10,
        region: 10
      })
    })

    it('should renormalize to 5:3:1 with fetch health N/A when URL unreachable', () => {
      const w = dimensionWeights(false)

      expect(w.fetchHealth).toBeNull()
      expect(w.availability).toBeCloseTo((5 / 9) * 100, 5)
      expect(w.latency).toBeCloseTo((3 / 9) * 100, 5)
      expect(w.region).toBeCloseTo((1 / 9) * 100, 5)
      // 重归一后总权重仍为 100%
      expect(w.availability + w.latency + w.region).toBeCloseTo(100, 5)
    })
  })

  describe('dimensionWeightsOf', () => {
    it('should prefer weights persisted on the run', () => {
      const w = dimensionWeightsOf({
        ...completedDims,
        availability_weight: 50,
        latency_weight: 30,
        fetch_health_weight: 10,
        region_weight: 10
      })

      expect(w).toEqual({ availability: 50, latency: 30, fetchHealth: 10, region: 10 })
    })

    it('should map null fetch_health_weight to N/A', () => {
      const w = dimensionWeightsOf({
        ...completedDims,
        url_reachable: false,
        availability_weight: (5 / 9) * 100,
        latency_weight: (3 / 9) * 100,
        fetch_health_weight: null,
        region_weight: (1 / 9) * 100
      })

      expect(w.fetchHealth).toBeNull()
      expect(w.availability).toBeCloseTo((5 / 9) * 100, 5)
    })

    it('should fall back to hardcoded weights for old runs without weight fields', () => {
      expect(dimensionWeightsOf(completedDims)).toEqual({
        availability: 50,
        latency: 30,
        fetchHealth: 10,
        region: 10
      })
      expect(dimensionWeightsOf({ ...completedDims, url_reachable: false }).fetchHealth).toBeNull()
      expect(dimensionWeightsOf(null)).toEqual(dimensionWeights(true))
      expect(dimensionWeightsOf(undefined)).toEqual(dimensionWeights(true))
    })
  })

  describe('weightLabel', () => {
    it('should render integer weights as whole percents', () => {
      expect(weightLabel(50)).toBe('50%')
      expect(weightLabel(10)).toBe('10%')
    })

    it('should render renormalized weights with one decimal', () => {
      expect(weightLabel((5 / 9) * 100)).toBe('55.6%')
      expect(weightLabel((1 / 9) * 100)).toBe('11.1%')
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
