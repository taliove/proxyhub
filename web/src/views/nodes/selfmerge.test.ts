import { describe, it, expect } from 'vitest'
import type { Node, SelfNode } from '@/types'
import { selfIdentity, selfNodeToRow, buildUnifiedRows, selfNodeIndex } from './selfmerge'
import { emptyCriteria, filterNodes } from './predicates'
import { SELF_HOSTED } from './utils'

const selfNode = (over: Partial<SelfNode> = {}): SelfNode => ({
  id: 1,
  name: '自建香港',
  protocol: 'trojan',
  server: '203.0.113.1',
  port: 443,
  uuid: '',
  password: 'p',
  cipher: '',
  alter_id: 0,
  network: 'tcp',
  tls: true,
  grpc_service_name: '',
  enabled: true,
  ...over
})

const poolNode = (over: Partial<Node> = {}): Node =>
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

describe('selfIdentity', () => {
  it('由 server/port/protocol 组成', () => {
    expect(selfIdentity('1.1.1.1', 443, 'trojan')).toBe(selfIdentity('1.1.1.1', 443, 'trojan'))
    expect(selfIdentity('1.1.1.1', 443, 'trojan')).not.toBe(selfIdentity('1.1.1.1', 80, 'trojan'))
  })
})

describe('selfNodeToRow', () => {
  it('禁用自建节点适配为行，携带 id/enabled 且 available=false', () => {
    const row = selfNodeToRow(selfNode({ id: 7, enabled: false }))
    expect(row.source).toBe(SELF_HOSTED)
    expect(row.self_node_id).toBe(7)
    expect(row.enabled).toBe(false)
    expect(row.available).toBe(false)
    expect(row.type).toBe('trojan')
    expect(row.node_key).toBe('self-node:7')
  })
})

describe('buildUnifiedRows', () => {
  it('机场行原样透传', () => {
    const rows = buildUnifiedRows([poolNode({ source: '机场A' })], [])
    expect(rows[0].self_node_id).toBeUndefined()
    expect(rows[0].enabled).toBeUndefined()
  })

  it('池中自建行按身份补 self_node_id 且 enabled=true', () => {
    const pool = [
      poolNode({
        source: SELF_HOSTED,
        type: 'trojan',
        server: '203.0.113.1',
        port: 443,
        node_key: 'x'
      })
    ]
    const rows = buildUnifiedRows(pool, [selfNode({ id: 9, enabled: true })])
    expect(rows[0].self_node_id).toBe(9)
    expect(rows[0].enabled).toBe(true)
  })

  it('禁用自建节点（不在池中）被补进表格', () => {
    const pool = [poolNode({ source: '机场A' })]
    const rows = buildUnifiedRows(pool, [selfNode({ id: 5, enabled: false })])
    expect(rows).toHaveLength(2)
    const disabled = rows.find((r) => r.self_node_id === 5)!
    expect(disabled.enabled).toBe(false)
    expect(disabled.source).toBe(SELF_HOSTED)
  })

  it('不改入参', () => {
    const pool = [
      poolNode({ source: SELF_HOSTED, type: 'trojan', server: '203.0.113.1', port: 443 })
    ]
    buildUnifiedRows(pool, [selfNode()])
    expect((pool[0] as { self_node_id?: number }).self_node_id).toBeUndefined()
  })
})

describe('selfNodeIndex', () => {
  it('按 id 建立索引', () => {
    const idx = selfNodeIndex([selfNode({ id: 1 }), selfNode({ id: 2, name: 'b' })])
    expect(idx.get(2)?.name).toBe('b')
    expect(idx.has(3)).toBe(false)
  })
})

// issue #50 回归钉死:来源筛选「自建节点」必须在合并行集上命中全部自建行
// (启用行来自池,禁用行由合并补入),而不是空白。
describe('buildUnifiedRows + 来源筛选(issue #50)', () => {
  it('source=self-hosted 命中启用与禁用自建行', () => {
    const pool = [
      poolNode({ source: '机场A', node_key: 'airport-1' }),
      poolNode({
        source: SELF_HOSTED,
        type: 'trojan',
        server: '203.0.113.1',
        port: 443,
        node_key: 'self-1'
      })
    ]
    const rows = buildUnifiedRows(pool, [
      selfNode({ id: 9, enabled: true }),
      selfNode({ id: 10, enabled: false, server: '203.0.113.2', name: '自建备用' })
    ])
    const matched = filterNodes(rows, { ...emptyCriteria(), source: SELF_HOSTED })
    const ids = matched.map((r) => (r as { self_node_id?: number }).self_node_id)
    expect(matched).toHaveLength(2)
    expect(ids).toContain(9)
    expect(ids).toContain(10)
    expect(matched.every((r) => r.source === SELF_HOSTED)).toBe(true)
  })
})
