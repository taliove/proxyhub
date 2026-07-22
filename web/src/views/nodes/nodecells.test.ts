import { describe, it, expect } from 'vitest'
import type { ExamHistoryEntry, ExamReport, Node } from '@/types'
import {
  nameCell,
  stateTags,
  healthTone,
  healthLabel,
  latencyText,
  tagsDisplay,
  examEgressCell,
  buildNodeExamSummary,
  unlockTargetsOf,
  tagsOf,
  regionDisplay
} from './nodecells'
import type { UnifiedNode } from './selfmerge'

const node = (over: Partial<Node> = {}): Node =>
  ({
    name: 'n',
    display_name: '',
    type: 'vmess',
    server: '1.1.1.1',
    port: 443,
    tls: true,
    region: 'HK',
    source: '机场A',
    latency: 100,
    available: true,
    node_key: 'k',
    blocked: false,
    stale: false,
    ...over
  }) as Node

const unode = (over: Partial<UnifiedNode> = {}): UnifiedNode => ({ ...node(), ...over })

describe('nameCell', () => {
  it('标准名优先,原始名作副标题', () => {
    expect(nameCell(node({ name: 'HK-01', display_name: '香港01' }))).toEqual({
      primary: '香港01',
      secondary: 'HK-01'
    })
  })
  it('无标准名时用原始名,不重复副标题', () => {
    expect(nameCell(node({ name: 'HK-01', display_name: '' }))).toEqual({
      primary: 'HK-01',
      secondary: ''
    })
  })
})

describe('stateTags', () => {
  it('正常可用节点无状态标签', () => {
    expect(stateTags(unode())).toEqual([])
  })
  it('屏蔽/下架叠加', () => {
    const tags = stateTags(unode({ blocked: true, stale: true }))
    expect(tags.map((t) => t.label)).toEqual(['已屏蔽', '已下架'])
  })
  it('禁用自建显示禁用', () => {
    expect(stateTags(unode({ enabled: false, available: false })).map((t) => t.label)).toEqual([
      '禁用'
    ])
  })
  it('不可用不出标签(由延迟列状态色点表达)', () => {
    expect(stateTags(unode({ available: false }))).toEqual([])
  })
})

describe('healthTone / healthLabel', () => {
  it('可用且有延迟为绿点', () => {
    expect(healthTone(unode({ available: true, latency: 88 }))).toBe('success')
    expect(healthLabel(unode({ available: true, latency: 88 }))).toBe('可用')
  })
  it('可用但无延迟为灰点(未检测)', () => {
    expect(healthTone(unode({ available: true, latency: 0 }))).toBe('muted')
    expect(healthLabel(unode({ available: true, latency: 0 }))).toBe('未检测')
  })
  it('不可用为红点(优先于延迟)', () => {
    expect(healthTone(unode({ available: false, latency: 88 }))).toBe('danger')
    expect(healthLabel(unode({ available: false, latency: 88 }))).toBe('不可用')
  })
  it('禁用为灰点(优先于可用性)', () => {
    expect(healthTone(unode({ enabled: false, available: false }))).toBe('muted')
    expect(healthLabel(unode({ enabled: false, available: false }))).toBe('已禁用')
  })
})

describe('latencyText', () => {
  it('有延迟显示毫秒', () => {
    expect(latencyText(node({ latency: 88 }))).toBe('88ms')
  })
  it('无延迟显示占位符', () => {
    expect(latencyText(node({ latency: 0 }))).toBe('—')
  })
})

describe('tagsDisplay - 空态', () => {
  it('缺省(票据 21 前)返回空数组', () => {
    expect(tagsDisplay(node({ tags: undefined }))).toEqual([])
  })
  it('有标签原样返回', () => {
    expect(tagsDisplay(node({ tags: ['流媒体'] }))).toEqual(['流媒体'])
  })
})

const report = (egress: ExamReport['egress']): ExamReport => ({ egress })

describe('examEgressCell', () => {
  it('无出网信息返回 null', () => {
    expect(examEgressCell(undefined)).toBeNull()
    expect(examEgressCell(report(undefined))).toBeNull()
  })
  it('仅国家码,无警示', () => {
    const c = examEgressCell(report({ ipv4: { country_code: 'us', proxy: false, hosting: false } }))
    expect(c).toEqual({ code: 'US', warn: false, reasons: [] })
  })
  it('代理/机房/DNS 泄露聚合为警示', () => {
    const c = examEgressCell(
      report({
        ipv4: { country_code: 'HK', proxy: true, hosting: true },
        dns: { leak: true }
      })
    )
    expect(c?.warn).toBe(true)
    expect(c?.reasons).toHaveLength(3)
  })
})

describe('buildNodeExamSummary', () => {
  const nowMs = Date.parse('2026-07-20T12:00:00Z')
  const entry = (report: ExamReport, created: string): ExamHistoryEntry => ({
    id: 1,
    node_key: 'k',
    report,
    created_at: created
  })

  it('无记录返回 null', () => {
    expect(buildNodeExamSummary(null)).toBeNull()
  })

  it('一次算出徽标/出网/相对时间', () => {
    const s = buildNodeExamSummary(
      entry(
        {
          stability: {
            total: 10,
            succeeded: 10,
            loss_rate: 0,
            mean_ms: 50,
            median_ms: 50,
            p95_ms: 60,
            p99_ms: 70,
            jitter_ms: 5,
            score: 92
          },
          egress: { ipv4: { country_code: 'JP', proxy: false, hosting: false } }
        },
        '2026-07-20T11:00:00Z'
      ),
      nowMs
    )
    expect(s?.badge?.score).toBe(92)
    expect(s?.egress?.code).toBe('JP')
    expect(s?.relative).toBe('1小时前')
  })
})

describe('unlockTargetsOf', () => {
  it('去重、排序、排除通用探测', () => {
    const nodes = [
      node({
        unlock_results: {
          Netflix: { available: true, latency: 1 },
          connectivity: { available: true, latency: 1 }
        }
      }),
      node({
        unlock_results: {
          YouTube: { available: false, latency: 1 },
          Netflix: { available: true, latency: 1 }
        }
      })
    ]
    expect(unlockTargetsOf(nodes)).toEqual(['Netflix', 'YouTube'])
  })
  it('无检测结果返回空', () => {
    expect(unlockTargetsOf([node({ unlock_results: undefined })])).toEqual([])
  })
})

describe('tagsOf', () => {
  it('去重排序', () => {
    expect(tagsOf([node({ tags: ['b', 'a'] }), node({ tags: ['a', 'c'] })])).toEqual([
      'a',
      'b',
      'c'
    ])
  })
  it('缺省返回空', () => {
    expect(tagsOf([node({ tags: undefined })])).toEqual([])
  })
})

describe('regionDisplay', () => {
  it('正常地区码显示原值', () => {
    expect(regionDisplay('HK')).toBe('HK')
    expect(regionDisplay('JP')).toBe('JP')
  })
  it('Unknown 显示未知', () => {
    expect(regionDisplay('Unknown')).toBe('未知')
  })
  it('空字符串显示未知', () => {
    expect(regionDisplay('')).toBe('未知')
  })
  it('undefined 显示未知', () => {
    expect(regionDisplay(undefined)).toBe('未知')
  })
})
