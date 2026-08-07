import { describe, it, expect } from 'vitest'
import { unlockLevel, unlockLabel, unlockColorVar, regionBadge, upsertUnlockRow } from './unlock'
import type { ExamUnlockResult } from '@/types'

const res = (target: string, over: Partial<ExamUnlockResult> = {}): ExamUnlockResult => ({
  node_key: 'n1',
  target_name: target,
  available: true,
  latency: 120,
  level: 'full',
  region: 'US',
  ...over
})

describe('unlockLevel', () => {
  it('maps the three known levels', () => {
    expect(unlockLevel(res('Netflix', { level: 'full' }))).toBe('full')
    expect(unlockLevel(res('Netflix', { level: 'originals_only' }))).toBe('originals_only')
    expect(unlockLevel(res('Netflix', { level: 'blocked' }))).toBe('blocked')
  })
  it('falls back to unknown for missing / unrecognized level', () => {
    expect(unlockLevel(res('X', { level: undefined }))).toBe('unknown')
    expect(unlockLevel(res('X', { level: '' }))).toBe('unknown')
    expect(unlockLevel(res('X', { level: 'weird' }))).toBe('unknown')
  })
})

describe('unlockLabel', () => {
  it('labels each level', () => {
    expect(unlockLabel(res('X', { level: 'full' }))).toBe('解锁')
    expect(unlockLabel(res('X', { level: 'originals_only' }))).toBe('自制剧')
    expect(unlockLabel(res('X', { level: 'blocked' }))).toBe('封锁')
  })
  it('unknown with error reads 不可用， otherwise 未知', () => {
    expect(unlockLabel(res('X', { level: undefined, error: 'boom', available: false }))).toBe(
      '不可用'
    )
    expect(unlockLabel(res('X', { level: undefined, error: undefined }))).toBe('未知')
  })
})

describe('unlockColorVar', () => {
  it('maps levels to green / yellow / red semantic tokens', () => {
    expect(unlockColorVar(res('X', { level: 'full' }))).toBe('--ph-success')
    expect(unlockColorVar(res('X', { level: 'originals_only' }))).toBe('--ph-warning')
    expect(unlockColorVar(res('X', { level: 'blocked' }))).toBe('--ph-danger')
  })
  it('unknown uses a neutral token', () => {
    expect(unlockColorVar(res('X', { level: undefined }))).toBe('--ph-text-secondary')
  })
})

describe('regionBadge', () => {
  it('uppercases and trims a present region', () => {
    expect(regionBadge(res('X', { region: 'us' }))).toBe('US')
    expect(regionBadge(res('X', { region: ' hk ' }))).toBe('HK')
  })
  it('returns empty string when region absent', () => {
    expect(regionBadge(res('X', { region: undefined }))).toBe('')
    expect(regionBadge(res('X', { region: '' }))).toBe('')
  })
})

describe('upsertUnlockRow', () => {
  it('appends a new target and preserves order', () => {
    const r1 = upsertUnlockRow([], res('Netflix'))
    const r2 = upsertUnlockRow(r1, res('OpenAI'))
    expect(r2.map((r) => r.target_name)).toEqual(['Netflix', 'OpenAI'])
  })

  it('replaces an existing target in place (immutably)', () => {
    const base = [res('Netflix', { level: 'full' }), res('OpenAI')]
    const next = upsertUnlockRow(base, res('Netflix', { level: 'blocked' }))
    expect(next).not.toBe(base)
    expect(next[0].level).toBe('blocked')
    expect(next.map((r) => r.target_name)).toEqual(['Netflix', 'OpenAI'])
    // 原数组未被修改
    expect(base[0].level).toBe('full')
  })
})
