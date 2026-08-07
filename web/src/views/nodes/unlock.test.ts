import { describe, it, expect } from 'vitest'
import {
  classifyUnlock,
  normalizeRegion,
  isGenericTarget,
  isGenericVariant,
  unlockDisplayRows,
  unlockSummary
} from './unlock'
import type { Node, UnlockResult } from '@/types'

const result = (over: Partial<UnlockResult> = {}): UnlockResult => ({
  available: true,
  latency: 120,
  ...over
})

const nodeWith = (unlock: Record<string, UnlockResult> | undefined): Node =>
  ({ unlock_results: unlock }) as unknown as Node

describe('classifyUnlock - 三档解锁着色', () => {
  it('full -> success 绿', () => {
    const d = classifyUnlock(result({ level: 'full' }), 'Netflix')
    expect(d.variant).toBe('full')
    expect(d.tagType).toBe('success')
    expect(d.label).toBe('完整解锁')
  })

  it('originals_only -> warning 黄', () => {
    const d = classifyUnlock(result({ level: 'originals_only' }), 'Netflix')
    expect(d.variant).toBe('originals_only')
    expect(d.tagType).toBe('warning')
    expect(d.label).toBe('仅自制剧')
  })

  it('blocked -> danger 红', () => {
    const d = classifyUnlock(result({ available: false, level: 'blocked' }), 'Netflix')
    expect(d.variant).toBe('blocked')
    expect(d.tagType).toBe('danger')
    expect(d.label).toBe('已封锁')
  })

  it('三档互不相同（绿/黄/红）', () => {
    const types = ['full', 'originals_only', 'blocked'].map(
      (level) => classifyUnlock(result({ available: false, level }), 'Netflix').tagType
    )
    expect(new Set(types).size).toBe(3)
  })
})

describe('classifyUnlock - error 与 blocked 区分', () => {
  it('解锁目标检测错误（无 level，有 error）-> info 灰，不误判为 blocked', () => {
    const d = classifyUnlock(result({ available: false, error: 'dial timeout' }), 'Netflix')
    expect(d.variant).toBe('error')
    expect(d.tagType).toBe('info')
    expect(d.label).toBe('检测失败')
    // 与 blocked 视觉可区分:tagType 不同
    expect(d.tagType).not.toBe('danger')
  })

  it('error 的 tagType 与 blocked 的 tagType 不同', () => {
    const err = classifyUnlock(result({ available: false, error: 'x' }), 'Netflix')
    const blocked = classifyUnlock(result({ available: false, level: 'blocked' }), 'Netflix')
    expect(err.tagType).not.toBe(blocked.tagType)
    expect(err.variant).not.toBe(blocked.variant)
  })
})

describe('classifyUnlock - generic 零回归', () => {
  it('通用探测（connectivity）可用 -> available/success,不进 error 档', () => {
    const d = classifyUnlock(result({ available: true }), 'connectivity')
    expect(d.variant).toBe('available')
    expect(d.tagType).toBe('success')
    expect(isGenericVariant(d.variant)).toBe(true)
  })

  it('通用探测失败（带 error）仍为 unavailable/danger,不变灰', () => {
    const d = classifyUnlock(result({ available: false, error: 'timeout' }), 'connectivity')
    expect(d.variant).toBe('unavailable')
    expect(d.tagType).toBe('danger')
  })

  it('bandwidth 也视为通用探测', () => {
    expect(isGenericTarget('bandwidth')).toBe(true)
    const d = classifyUnlock(result({ available: false, error: 'x' }), 'bandwidth')
    expect(d.variant).toBe('unavailable')
  })

  it('无 level 无 error 的普通结果按 available/unavailable 分档', () => {
    expect(classifyUnlock(result({ available: true }), 'YouTube').variant).toBe('available')
    expect(classifyUnlock(result({ available: false }), 'YouTube').variant).toBe('unavailable')
  })
})

describe('normalizeRegion - 国家码徽标有无', () => {
  it('非空规范为两位大写', () => {
    expect(normalizeRegion('us')).toBe('US')
    expect(normalizeRegion(' hk ')).toBe('HK')
  })

  it('空/未定义/空白返回空串（不占位）', () => {
    expect(normalizeRegion('')).toBe('')
    expect(normalizeRegion(undefined)).toBe('')
    expect(normalizeRegion('   ')).toBe('')
  })
})

describe('isGenericVariant', () => {
  it('仅 available/unavailable 为通用档', () => {
    expect(isGenericVariant('available')).toBe(true)
    expect(isGenericVariant('unavailable')).toBe(true)
    expect(isGenericVariant('full')).toBe(false)
    expect(isGenericVariant('blocked')).toBe(false)
    expect(isGenericVariant('error')).toBe(false)
  })
})

describe('unlockDisplayRows - 展开并附带分档/徽标', () => {
  it('无检测结果返回空数组', () => {
    expect(unlockDisplayRows(nodeWith(undefined))).toEqual([])
  })

  it('每个目标携带 display 分档与规范化 region', () => {
    const rows = unlockDisplayRows(
      nodeWith({
        Netflix: result({ level: 'originals_only', region: 'sg' }),
        OpenAI: result({ available: false, error: 'blocked by 403' })
      })
    )
    const netflix = rows.find((r) => r.target === 'Netflix')!
    expect(netflix.display.variant).toBe('originals_only')
    expect(netflix.region).toBe('SG')

    const openai = rows.find((r) => r.target === 'OpenAI')!
    expect(openai.display.variant).toBe('error')
    expect(openai.region).toBe('')
  })
})

describe('unlockSummary - 通过数汇总（既有行为不回归）', () => {
  it('无结果返回占位符', () => {
    expect(unlockSummary(nodeWith(undefined))).toBe('—')
  })

  it('统计 available 命中数', () => {
    const s = unlockSummary(
      nodeWith({
        Netflix: result({ available: true, level: 'full' }),
        Disney: result({ available: false, level: 'blocked' }),
        OpenAI: result({ available: true })
      })
    )
    expect(s).toBe('2/3')
  })
})
