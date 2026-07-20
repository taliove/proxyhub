import { describe, it, expect } from 'vitest'
import { EXAM_LOG_MAX, examLogLine, appendExamLog, buildExamLog } from './examlog'
import type { ExamEvent } from '@/types'

describe('examLogLine', () => {
  it('renders a successful sample line', () => {
    const f: ExamEvent = {
      phase: 'sample',
      sample: { seq: 12, elapsed_ms: 0, latency_ms: 163, ok: true }
    }
    expect(examLogLine(f)).toBe('采样 #12: 163ms')
  })

  it('renders a lost sample as timeout', () => {
    const f: ExamEvent = {
      phase: 'sample',
      sample: { seq: 5, elapsed_ms: 0, latency_ms: 0, ok: false }
    }
    expect(examLogLine(f)).toBe('采样 #5: 超时')
  })

  it('renders a region result with latency and speed', () => {
    const f: ExamEvent = {
      phase: 'region',
      region: { code: 'jp', name: '东京', ttfb_ms: 45, down_mbps: 23.4 }
    }
    expect(examLogLine(f)).toBe('东京: 45ms · 23.4 Mbps')
  })

  it('renders a failed region', () => {
    const f: ExamEvent = {
      phase: 'region',
      region: { code: 'jp', name: '东京', ttfb_ms: 0, down_mbps: 0, error: 'timeout' }
    }
    expect(examLogLine(f)).toBe('东京: 失败')
  })

  it('renders an unlock result with region', () => {
    const f: ExamEvent = {
      phase: 'unlock',
      unlock_result: {
        node_key: 'k',
        target_name: 'Netflix',
        available: true,
        latency: 100,
        level: 'full',
        region: 'jp'
      }
    }
    expect(examLogLine(f)).toBe('Netflix: 解锁 JP')
  })

  it('renders an unlock result without region', () => {
    const f: ExamEvent = {
      phase: 'unlock',
      unlock_result: {
        node_key: 'k',
        target_name: 'Disney+',
        available: false,
        latency: 0,
        level: 'blocked'
      }
    }
    expect(examLogLine(f)).toBe('Disney+: 封锁')
  })

  it('renders terminal and section lines', () => {
    expect(examLogLine({ phase: 'section_done', section: 'stability' })).toBe('稳定性采样完成')
    expect(examLogLine({ phase: 'section_done', section: 'region_speed' })).toBe('多地域测速完成')
    expect(examLogLine({ phase: 'section_done', section: 'unlock' })).toBe('解锁检测完成')
    expect(examLogLine({ phase: 'done' })).toBe('体检完成')
    expect(examLogLine({ phase: 'cancelled' })).toBe('已取消')
    expect(examLogLine({ phase: 'error', error: '连接失败' })).toBe('体检失败: 连接失败')
  })

  it('returns null for frames without a log payload', () => {
    expect(examLogLine({ phase: 'sample' })).toBeNull()
    expect(examLogLine({ phase: 'region' })).toBeNull()
  })
})

describe('appendExamLog', () => {
  it('appends immutably and keeps the last N', () => {
    const base = ['a', 'b']
    const next = appendExamLog(base, 'c')
    expect(next).toEqual(['a', 'b', 'c'])
    expect(base).toEqual(['a', 'b'])
  })

  it('trims to EXAM_LOG_MAX oldest-first', () => {
    let lines: string[] = []
    for (let i = 1; i <= EXAM_LOG_MAX + 3; i++) lines = appendExamLog(lines, `#${i}`)
    expect(lines).toHaveLength(EXAM_LOG_MAX)
    expect(lines[0]).toBe(`#4`)
    expect(lines[lines.length - 1]).toBe(`#${EXAM_LOG_MAX + 3}`)
  })

  it('is a no-op for null (identity array returned)', () => {
    const base = ['a']
    expect(appendExamLog(base, null)).toBe(base)
  })
})

describe('buildExamLog', () => {
  it('folds a frame sequence into the last N human lines', () => {
    const frames: ExamEvent[] = [
      { phase: 'sample', sample: { seq: 1, elapsed_ms: 0, latency_ms: 100, ok: true } },
      { phase: 'sample', sample: { seq: 2, elapsed_ms: 0, latency_ms: 0, ok: false } },
      { phase: 'region', region: { code: 'jp', name: '东京', ttfb_ms: 45, down_mbps: 23.4 } },
      { phase: 'done' }
    ]
    expect(buildExamLog(frames)).toEqual([
      '采样 #1: 100ms',
      '采样 #2: 超时',
      '东京: 45ms · 23.4 Mbps',
      '体检完成'
    ])
  })
})
