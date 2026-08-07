import { describe, it, expect } from 'vitest'
import type { Node } from '@/types'
import { availabilitySourceText, subscriptionHint } from './utils'

// 与后端 nodeView 字段对齐的最小 fixture(见 ticket 0016)
const node = (over: Partial<Node> = {}): Node =>
  ({
    name: 'n',
    display_name: '',
    type: 'ss',
    server: 'a.example.com',
    port: 8388,
    tls: false,
    region: 'HK',
    source: '机场A',
    latency: 0,
    available: false,
    node_key: 'a.example.com:8388',
    blocked: false,
    stale: false,
    availability_source: 'never',
    ...over
  }) as Node

describe('availabilitySourceText', () => {
  it('三态文案与后端枚举对齐', () => {
    expect(availabilitySourceText(node({ availability_source: 'never' }))).toBe('从未检测')
    expect(availabilitySourceText(node({ availability_source: 'health' }))).toBe(
      '仅健康检查（TCP 快检）'
    )
    expect(availabilitySourceText(node({ availability_source: 'real' }))).toBe(
      '真实检测（代理请求）'
    )
  })

  it('未知值按"从未检测"兜底（与后端口径一致）', () => {
    expect(availabilitySourceText(node({ availability_source: 'bogus' as never }))).toBe('从未检测')
  })
})

describe('subscriptionHint', () => {
  it('自建节点豁免过滤，不误导为被剔除', () => {
    expect(subscriptionHint(node({ source: 'self-hosted' }))).toContain('豁免')
  })

  it('屏蔽优先于不可用展示', () => {
    const hint = subscriptionHint(node({ blocked: true, available: false }))
    expect(hint).toContain('屏蔽')
  })

  it('stale 节点说明已从机场订阅消失', () => {
    expect(subscriptionHint(node({ stale: true }))).toContain('消失')
  })

  it('从未检测的不可用节点给出翻牌路径', () => {
    const hint = subscriptionHint(node({ available: false, availability_source: 'never' }))
    expect(hint).toContain('尚未跑过任何检测')
  })

  it('不可用节点带判定来源', () => {
    const hint = subscriptionHint(node({ available: false, availability_source: 'real' }))
    expect(hint).toContain('真实检测')
  })

  it('可用节点提示仍受过滤影响', () => {
    expect(subscriptionHint(node({ available: true }))).toContain('可用')
  })
})
