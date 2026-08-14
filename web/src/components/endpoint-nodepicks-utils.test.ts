import { describe, it, expect } from 'vitest'
import type { Node } from '@/types'
import {
  parseNodePicks,
  nodePicksCount,
  nodePicksLabel,
  nodePicksBroken,
  filterPicksPool,
  paginateSlice,
  mergePicks,
  filterEndpointsByPicks,
  type NodePick
} from './endpoint-nodepicks-utils'

// nodePicksBroken(issue #91):以后端 picks_error 标记为单一事实源
describe('nodePicksBroken 精选损坏标记', () => {
  it('picks_error === true 才算损坏', () => {
    expect(nodePicksBroken({ picks_error: true })).toBe(true)
    expect(nodePicksBroken({ picks_error: false })).toBe(false)
    expect(nodePicksBroken({})).toBe(false)
  })
})

// 订阅地址精选纯函数缝(issue #85 双格式解析;#86 池过滤/分页/全选)。
// fixture 全合成(example.com)。

describe('parseNodePicks 双格式兼容（issue #85）', () => {
  it('旧格式字符串数组解析为无别名项', () => {
    expect(parseNodePicks('["a.example.com:8388","b.example.com:443"]')).toEqual([
      { key: 'a.example.com:8388' },
      { key: 'b.example.com:443' }
    ])
  })

  it('新格式对象数组解析 key 与 alias', () => {
    expect(
      parseNodePicks(
        '[{"key":"a.example.com:8388","alias":"老爸的香港"},{"key":"b.example.com:443"}]'
      )
    ).toEqual([{ key: 'a.example.com:8388', alias: '老爸的香港' }, { key: 'b.example.com:443' }])
  })

  it('新旧混合格式可解析；空 alias 不落字段', () => {
    expect(parseNodePicks('[{"key":"a.example.com:8388","alias":""},"b.example.com:443"]')).toEqual(
      [{ key: 'a.example.com:8388' }, { key: 'b.example.com:443' }]
    )
  })

  it('空串/缺省/非法 JSON/非数组按空集（与后端降级语义一致）', () => {
    expect(parseNodePicks('')).toEqual([])
    expect(parseNodePicks(undefined)).toEqual([])
    expect(parseNodePicks('not-json')).toEqual([])
    expect(parseNodePicks('{"a":1}')).toEqual([])
  })

  it('非法元素形状被过滤，合法元素保留', () => {
    expect(parseNodePicks('[1,"a.example.com:8388",{"nokey":true},null]')).toEqual([
      { key: 'a.example.com:8388' }
    ])
  })

  it('数量与文案：0 = 全量', () => {
    expect(nodePicksCount({ node_picks: '[{"key":"a.example.com:8388"}]' })).toBe(1)
    expect(nodePicksLabel({ node_picks: '[{"key":"a.example.com:8388"}]' })).toBe('精选 1 个节点')
    expect(nodePicksLabel({ node_picks: '' })).toBe('全量')
  })
})

const poolNode = (over: Partial<Node>): Node =>
  ({
    node_key: 'a.example.com:8388',
    name: '香港A',
    display_name: '',
    source: '机场甲',
    region: 'HK',
    type: 'ss',
    server: 'a.example.com',
    port: 8388,
    latency: 0,
    available: true,
    favorite: false,
    ...over
  }) as Node

describe('filterPicksPool 页签 + 关键字（issue #86）', () => {
  const pool = [
    poolNode({ node_key: 'a.example.com:8388', name: '香港A' }),
    poolNode({ node_key: 'self.example.com:443', name: '家里宽带', source: 'self-hosted' }),
    poolNode({ node_key: 'fav.example.com:8443', name: '收藏的日本', favorite: true }),
    poolNode({ node_key: 'b.example.com:443', name: '美国C', region: 'US' })
  ]

  it('all 页签不过滤', () => {
    expect(filterPicksPool(pool, 'all', '')).toHaveLength(4)
  })

  it('self 页签只看自建节点（source 精确 self-hosted）', () => {
    const out = filterPicksPool(pool, 'self', '')
    expect(out.map((n) => n.node_key)).toEqual(['self.example.com:443'])
  })

  it('fav 页签只看已收藏（favorite 缺省按未收藏）', () => {
    const out = filterPicksPool(pool, 'fav', '')
    expect(out.map((n) => n.node_key)).toEqual(['fav.example.com:8443'])
  })

  it('关键字跨名称/来源/地区子串匹配，可与页签叠加；大小写不敏感', () => {
    expect(filterPicksPool(pool, 'all', '香港').map((n) => n.node_key)).toEqual([
      'a.example.com:8388'
    ])
    expect(filterPicksPool(pool, 'all', 'us').map((n) => n.node_key)).toEqual(['b.example.com:443'])
    expect(filterPicksPool(pool, 'fav', '日本').map((n) => n.node_key)).toEqual([
      'fav.example.com:8443'
    ])
    expect(filterPicksPool(pool, 'self', '香港')).toHaveLength(0)
  })
})

describe('paginateSlice 前端分页（issue #86）', () => {
  const items = Array.from({ length: 120 }, (_, i) => i)

  it('按页切片：首页/中间页/尾页（不满页）', () => {
    expect(paginateSlice(items, 1, 50)).toHaveLength(50)
    expect(paginateSlice(items, 2, 50)[0]).toBe(50)
    expect(paginateSlice(items, 3, 50)).toHaveLength(20)
  })

  it('越界页收敛到最后一页；页码下界为 1', () => {
    expect(paginateSlice(items, 99, 50)).toEqual(paginateSlice(items, 3, 50))
    expect(paginateSlice(items, 0, 50)).toEqual(paginateSlice(items, 1, 50))
  })

  it('空列表恒返回空切片', () => {
    expect(paginateSlice([], 1, 50)).toEqual([])
  })
})

describe('filterEndpointsByPicks 订阅列表精选筛选（issue #87）', () => {
  const eps = [
    { id: 1, node_picks: '[{"key":"a.example.com:8388"}]' },
    { id: 2, node_picks: '' },
    { id: 3, node_picks: 'bad-json' }, // 损坏按空集（=全量）归类，与降级语义一致
    { id: 4, node_picks: '["b.example.com:443"]' } // 旧格式同样算已精选
  ]

  it('all 原样返回（不改入参数组引用）', () => {
    expect(filterEndpointsByPicks(eps, 'all')).toBe(eps)
  })

  it('picked 只留已精选（新旧格式都算）;full 只留全量（含损坏降级）', () => {
    expect(filterEndpointsByPicks(eps, 'picked').map((e) => e.id)).toEqual([1, 4])
    expect(filterEndpointsByPicks(eps, 'full').map((e) => e.id)).toEqual([2, 3])
  })
})

describe('mergePicks 全选当前过滤结果（issue #86）', () => {
  it('批量并入按 key 去重，已在选项保留原别名', () => {
    const selected: NodePick[] = [{ key: 'a.example.com:8388', alias: '已有别名' }]
    const merged = mergePicks(selected, ['a.example.com:8388', 'b.example.com:443'])
    expect(merged).toEqual([
      { key: 'a.example.com:8388', alias: '已有别名' },
      { key: 'b.example.com:443' }
    ])
  })

  it('重复执行幂等（再点一次不重复）', () => {
    const once = mergePicks([], ['a.example.com:8388'])
    expect(mergePicks(once, ['a.example.com:8388'])).toEqual(once)
  })

  it('不改入参', () => {
    const selected: NodePick[] = []
    mergePicks(selected, ['a.example.com:8388'])
    expect(selected).toEqual([])
  })
})
