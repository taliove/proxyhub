import { describe, it, expect } from 'vitest'
import { parseDiagnosticResult, formatDuration, isHttpSuccess } from './useAirportTest'

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
})
