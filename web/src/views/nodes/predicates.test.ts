import { describe, it, expect } from 'vitest'
import type { Node, UnlockResult } from '@/types'
import {
  emptyCriteria,
  matchesSource,
  matchesRegion,
  matchesType,
  matchesFlag,
  matchesKeyword,
  matchesTags,
  matchesUnlock,
  matchesStabilityBand,
  matchesNode,
  filterNodes,
  isActiveCriteria,
  sortNodes,
  type NodeFilterCriteria
} from './predicates'
import { SELF_HOSTED } from './utils'

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

const unlock = (over: Partial<UnlockResult> = {}): UnlockResult => ({
  available: true,
  latency: 50,
  ...over
})

const criteria = (over: Partial<NodeFilterCriteria> = {}): NodeFilterCriteria => ({
  ...emptyCriteria(),
  ...over
})

describe('matchesSource', () => {
  it('空来源匹配所有', () => {
    expect(matchesSource(node(), '')).toBe(true)
  })
  it('self-hosted 仅匹配自建节点', () => {
    expect(matchesSource(node({ source: SELF_HOSTED }), SELF_HOSTED)).toBe(true)
    expect(matchesSource(node({ source: '机场A' }), SELF_HOSTED)).toBe(false)
  })
  it('具体机场名精确匹配', () => {
    expect(matchesSource(node({ source: '机场A' }), '机场A')).toBe(true)
    expect(matchesSource(node({ source: '机场B' }), '机场A')).toBe(false)
  })
})

describe('matchesRegion', () => {
  it('大小写不敏感精确匹配', () => {
    expect(matchesRegion(node({ region: 'HK' }), 'hk')).toBe(true)
    expect(matchesRegion(node({ region: 'US' }), 'HK')).toBe(false)
  })
  it('空条件匹配所有', () => {
    expect(matchesRegion(node({ region: '' }), '')).toBe(true)
  })
})

describe('matchesType', () => {
  it('精确匹配协议', () => {
    expect(matchesType(node({ type: 'trojan' }), 'trojan')).toBe(true)
    expect(matchesType(node({ type: 'vmess' }), 'trojan')).toBe(false)
  })
})

describe('matchesFlag - 三态布尔', () => {
  it('null 不筛选', () => {
    expect(matchesFlag(true, null)).toBe(true)
    expect(matchesFlag(false, null)).toBe(true)
  })
  it('值相等才命中', () => {
    expect(matchesFlag(true, true)).toBe(true)
    expect(matchesFlag(false, true)).toBe(false)
  })
})

describe('matchesKeyword', () => {
  it('命中名称/标准名/服务器,大小写不敏感', () => {
    const n = node({ name: 'Tokyo-01', display_name: '日本东京', server: '203.0.113.9' })
    expect(matchesKeyword(n, 'tokyo')).toBe(true)
    expect(matchesKeyword(n, '东京')).toBe(true)
    expect(matchesKeyword(n, '203.0')).toBe(true)
    expect(matchesKeyword(n, 'osaka')).toBe(false)
  })
  it('空/空白关键词匹配所有', () => {
    expect(matchesKeyword(node(), '   ')).toBe(true)
  })
})

describe('matchesTags - OR 语义', () => {
  it('空条件匹配所有', () => {
    expect(matchesTags(node({ tags: [] }), [])).toBe(true)
  })
  it('任一标签命中即匹配', () => {
    const n = node({ tags: ['流媒体', '低延迟'] })
    expect(matchesTags(n, ['低延迟'])).toBe(true)
    expect(matchesTags(n, ['游戏', '低延迟'])).toBe(true)
    expect(matchesTags(n, ['游戏'])).toBe(false)
  })
  it('节点无 tags 字段(票据 21 前)时,启用标签筛选不报错且不命中', () => {
    expect(matchesTags(node({ tags: undefined }), ['流媒体'])).toBe(false)
  })
})

describe('matchesUnlock - AND 语义', () => {
  it('空条件匹配所有', () => {
    expect(matchesUnlock(node(), [])).toBe(true)
  })
  it('要求所选目标全部已解锁', () => {
    const n = node({
      unlock_results: {
        Netflix: unlock({ available: true }),
        YouTube: unlock({ available: true }),
        Disney: unlock({ available: false })
      }
    })
    expect(matchesUnlock(n, ['Netflix', 'YouTube'])).toBe(true)
    expect(matchesUnlock(n, ['Netflix', 'Disney'])).toBe(false)
  })
  it('无检测结果时,启用解锁筛选不命中', () => {
    expect(matchesUnlock(node({ unlock_results: undefined }), ['Netflix'])).toBe(false)
  })
})

describe('matchesStabilityBand', () => {
  it('无分档条件匹配所有', () => {
    expect(matchesStabilityBand('k', null, {})).toBe(true)
  })
  it('分档相等才命中', () => {
    expect(matchesStabilityBand('k', 'good', { k: 'good' })).toBe(true)
    expect(matchesStabilityBand('k', 'good', { k: 'fair' })).toBe(false)
  })
  it('无已加载分档的节点在启用筛选时不命中', () => {
    expect(matchesStabilityBand('k', 'good', {})).toBe(false)
  })
})

describe('matchesNode / filterNodes - 组合', () => {
  it('多条件 AND 组合', () => {
    const n = node({ source: '机场A', region: 'HK', type: 'trojan', available: true })
    expect(matchesNode(n, criteria({ source: '机场A', region: 'HK', type: 'trojan' }))).toBe(true)
    expect(matchesNode(n, criteria({ source: '机场A', type: 'vmess' }))).toBe(false)
  })

  it('filterNodes 返回新数组,不改入参', () => {
    const nodes = [node({ node_key: 'a', region: 'HK' }), node({ node_key: 'b', region: 'US' })]
    const out = filterNodes(nodes, criteria({ region: 'HK' }))
    expect(out.map((n) => n.node_key)).toEqual(['a'])
    expect(nodes).toHaveLength(2) // 入参未变
  })

  it('稳定性分档经 context 注入', () => {
    const nodes = [node({ node_key: 'a' }), node({ node_key: 'b' })]
    const out = filterNodes(nodes, criteria({ stabilityBand: 'good' }), {
      bandByKey: { a: 'good', b: 'poor' }
    })
    expect(out.map((n) => n.node_key)).toEqual(['a'])
  })
})

describe('isActiveCriteria', () => {
  it('空条件为非激活', () => {
    expect(isActiveCriteria(emptyCriteria())).toBe(false)
  })
  it('任一维度有值即激活', () => {
    expect(isActiveCriteria(criteria({ region: 'HK' }))).toBe(true)
    expect(isActiveCriteria(criteria({ available: false }))).toBe(true)
    expect(isActiveCriteria(criteria({ tags: ['x'] }))).toBe(true)
    expect(isActiveCriteria(criteria({ keyword: '  ' }))).toBe(false)
  })
})

describe('sortNodes - 不原地改 + 稳定次级键', () => {
  it('按延迟升序,node_key 作次级键', () => {
    const nodes = [
      node({ node_key: 'b', latency: 100 }),
      node({ node_key: 'a', latency: 100 }),
      node({ node_key: 'c', latency: 50 })
    ]
    const out = sortNodes(nodes, 'latency', 'asc')
    expect(out.map((n) => n.node_key)).toEqual(['c', 'a', 'b'])
    expect(nodes[0].node_key).toBe('b') // 入参未变
  })
  it('降序反转主键', () => {
    const nodes = [node({ node_key: 'a', latency: 50 }), node({ node_key: 'b', latency: 100 })]
    expect(sortNodes(nodes, 'latency', 'desc').map((n) => n.node_key)).toEqual(['b', 'a'])
  })
  it('按 source 排序', () => {
    const nodes = [
      node({ node_key: 'a', source: '机场B' }),
      node({ node_key: 'b', source: '机场A' })
    ]
    expect(sortNodes(nodes, 'source', 'asc').map((n) => n.source)).toEqual(['机场A', '机场B'])
  })
})
