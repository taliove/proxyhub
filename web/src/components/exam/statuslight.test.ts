import { describe, it, expect } from 'vitest'
import { computeStatusLight } from './statuslight'
import type { ExamRegionResult, ExamUnlockResult, ExamEgressMetrics } from '@/types'

describe('computeStatusLight', () => {
  it('returns red for connection error status', () => {
    expect(computeStatusLight('error', '', null, [], [], null)).toBe('red')
  })

  it('returns red when terminalError is present', () => {
    expect(computeStatusLight('live', 'connection failed', null, [], [], null)).toBe('red')
  })

  it('returns green for idle/connecting/live with no errors', () => {
    expect(computeStatusLight('idle', '', null, [], [], null)).toBe('green')
    expect(computeStatusLight('connecting', '', null, [], [], null)).toBe('green')
    expect(computeStatusLight('live', '', null, [], [], null)).toBe('green')
    expect(computeStatusLight('done', '', null, [], [], null)).toBe('green')
  })

  it('returns yellow when region has error', () => {
    const regions: ExamRegionResult[] = [
      { code: 'US', name: 'USA', ttfb_ms: 0, down_mbps: 0, error: 'timeout' }
    ]
    expect(computeStatusLight('live', '', null, regions, [], null)).toBe('yellow')
  })

  it('returns yellow when unlock has error', () => {
    const unlocks: ExamUnlockResult[] = [
      {
        node_key: 'k1',
        target_name: 'Netflix',
        available: false,
        latency: 0,
        error: 'blocked'
      }
    ]
    expect(computeStatusLight('live', '', null, [], unlocks, null)).toBe('yellow')
  })

  it('returns yellow when egress ipv4 has error', () => {
    const egress: ExamEgressMetrics = {
      ipv4: { proxy: false, hosting: false, error: 'lookup failed' }
    }
    expect(computeStatusLight('live', '', null, [], [], egress)).toBe('yellow')
  })

  it('returns yellow when egress ipv6 has error', () => {
    const egress: ExamEgressMetrics = {
      ipv6: { available: false, error: 'no route' }
    }
    expect(computeStatusLight('live', '', null, [], [], egress)).toBe('yellow')
  })

  it('returns yellow when egress dns has error', () => {
    const egress: ExamEgressMetrics = {
      dns: { leak: false, error: 'resolver unreachable' }
    }
    expect(computeStatusLight('live', '', null, [], [], egress)).toBe('yellow')
  })

  it('returns yellow when multiple sections have errors', () => {
    const regions: ExamRegionResult[] = [
      { code: 'US', name: 'USA', ttfb_ms: 0, down_mbps: 0, error: 'timeout' }
    ]
    const unlocks: ExamUnlockResult[] = [
      {
        node_key: 'k1',
        target_name: 'Netflix',
        available: false,
        latency: 0,
        error: 'blocked'
      }
    ]
    expect(computeStatusLight('live', '', null, regions, unlocks, null)).toBe('yellow')
  })

  it('returns green even after done if no errors', () => {
    const regions: ExamRegionResult[] = [{ code: 'US', name: 'USA', ttfb_ms: 50, down_mbps: 100 }]
    const unlocks: ExamUnlockResult[] = [
      { node_key: 'k1', target_name: 'Netflix', available: true, latency: 100 }
    ]
    const egress: ExamEgressMetrics = {
      ipv4: { proxy: false, hosting: false, ip: '1.2.3.4' }
    }
    expect(computeStatusLight('done', '', null, regions, unlocks, egress)).toBe('green')
  })

  it('prioritizes red over yellow (terminal error overrides data errors)', () => {
    const regions: ExamRegionResult[] = [
      { code: 'US', name: 'USA', ttfb_ms: 0, down_mbps: 0, error: 'timeout' }
    ]
    expect(computeStatusLight('error', 'connection lost', null, regions, [], null)).toBe('red')
  })
})
