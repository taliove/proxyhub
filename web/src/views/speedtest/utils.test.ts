import { describe, it, expect } from 'vitest'
import type { SpeedtestResult } from '@/api/speedtest'
import {
  computeLatencyMetrics,
  mbpsFromBytes,
  aggregateBucket,
  toAggregateViews,
  isOrphanKey,
  round2,
  DIRECT_KEY
} from './utils'

const record = (over: Partial<SpeedtestResult> = {}): SpeedtestResult => ({
  id: 1,
  node_key: 'hk.example.com:443',
  down_mbps: 100,
  up_mbps: 50,
  idle_latency_ms: 40,
  jitter_ms: 5,
  client_info: 'test-agent',
  created_at: '2026-07-23T10:00:00Z',
  ...over
})

describe('computeLatencyMetrics', () => {
  it('空闲延迟取最小 RTT,抖动取相邻差绝对值均值', () => {
    const m = computeLatencyMetrics([50, 40, 60, 45])
    expect(m.idleLatencyMs).toBe(40)
    // |40-50| + |60-40| + |45-60| = 10+20+15, 均值 15
    expect(m.jitterMs).toBe(15)
  })

  it('单样本抖动为 0,空样本全 0', () => {
    expect(computeLatencyMetrics([30])).toEqual({ idleLatencyMs: 30, jitterMs: 0 })
    expect(computeLatencyMetrics([])).toEqual({ idleLatencyMs: 0, jitterMs: 0 })
  })
})

describe('mbpsFromBytes', () => {
  it('字节/耗时换算 Mbps(1e6 bit/s)', () => {
    // 1 MB in 1s = 8 Mbps
    expect(mbpsFromBytes(1_000_000, 1000)).toBe(8)
  })

  it('零耗时或零字节返回 0,不产生 NaN/Infinity', () => {
    expect(mbpsFromBytes(1000, 0)).toBe(0)
    expect(mbpsFromBytes(0, 1000)).toBe(0)
  })
})

describe('aggregateBucket', () => {
  it('均值口径聚合,latestAt 取桶内最新时间', () => {
    const agg = aggregateBucket('k', [
      record({ down_mbps: 100, up_mbps: 40, created_at: '2026-07-23T10:00:00Z' }),
      record({ down_mbps: 200, up_mbps: 60, created_at: '2026-07-23T11:00:00Z' })
    ])
    expect(agg).toMatchObject({
      nodeKey: 'k',
      count: 2,
      downMbps: 150,
      upMbps: 50,
      latestAt: '2026-07-23T11:00:00Z'
    })
  })

  it('空桶返回 null', () => {
    expect(aggregateBucket('k', [])).toBeNull()
  })
})

describe('toAggregateViews', () => {
  it('按节点聚合并附与直连基线的差值(节点开销 = 经节点 - 直连)', () => {
    const views = toAggregateViews([
      record({
        node_key: DIRECT_KEY,
        down_mbps: 200,
        up_mbps: 100,
        idle_latency_ms: 20,
        jitter_ms: 2
      }),
      record({ node_key: 'hk:443', down_mbps: 150, up_mbps: 80, idle_latency_ms: 50, jitter_ms: 6 })
    ])
    expect(views).toHaveLength(2)
    const direct = views.find((v) => v.isDirect)!
    expect(direct.deltaDownMbps).toBeNull()
    const node = views.find((v) => v.nodeKey === 'hk:443')!
    expect(node.deltaDownMbps).toBe(-50)
    expect(node.deltaUpMbps).toBe(-20)
    expect(node.deltaLatencyMs).toBe(30)
    expect(node.deltaJitterMs).toBe(4)
  })

  it('无直连基线时差值为 null,不编造 0', () => {
    const views = toAggregateViews([record({ node_key: 'hk:443' })])
    expect(views[0].deltaDownMbps).toBeNull()
    expect(views[0].deltaLatencyMs).toBeNull()
  })

  it('直连行固定最前,其余按最近实测倒序;孤儿桶照常聚合', () => {
    const views = toAggregateViews([
      record({ node_key: 'old:1', created_at: '2026-07-20T10:00:00Z' }),
      record({ node_key: DIRECT_KEY, created_at: '2026-07-21T10:00:00Z' }),
      record({ node_key: 'new:2', created_at: '2026-07-23T10:00:00Z' })
    ])
    expect(views.map((v) => v.nodeKey)).toEqual([DIRECT_KEY, 'new:2', 'old:1'])
  })

  it('不改入参(不可变)', () => {
    const input = [record({ node_key: 'a:1' }), record({ node_key: 'a:1', id: 2 })]
    const snapshot = JSON.parse(JSON.stringify(input))
    toAggregateViews(input)
    expect(input).toEqual(snapshot)
  })
})

describe('isOrphanKey', () => {
  it('标注 key 不在池即孤儿;直连桶永远不是孤儿', () => {
    const pool = new Set(['hk:443'])
    expect(isOrphanKey('gone:1', pool)).toBe(true)
    expect(isOrphanKey('hk:443', pool)).toBe(false)
    expect(isOrphanKey(DIRECT_KEY, pool)).toBe(false)
  })
})

describe('round2', () => {
  it('两位小数', () => {
    expect(round2(1.006)).toBe(1.01)
    expect(round2(-3.456)).toBe(-3.46)
  })
})
