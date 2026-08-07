import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { useNodeQuery } from './useNodeQuery'
import type { UnifiedNode } from '../selfmerge'

const node = (over: Partial<UnifiedNode> = {}): UnifiedNode =>
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
  }) as UnifiedNode

describe('useNodeQuery - 稳定性分档筛选（端到端接线）', () => {
  it('由节点 stability_score 派生分档并过滤（优 >=85）', () => {
    const rows = ref<UnifiedNode[]>([
      node({ node_key: 'good', stability_score: 92 }),
      node({ node_key: 'fair', stability_score: 70 }),
      node({ node_key: 'poor', stability_score: 30 })
    ])
    const q = useNodeQuery(rows)
    q.criteria.stabilityBand = 'good'
    expect(q.pagedNodes.value.map((n) => n.node_key)).toEqual(['good'])
  })

  it('分档筛选命中"差"(<60,含 0 分)', () => {
    const rows = ref<UnifiedNode[]>([
      node({ node_key: 'good', stability_score: 90 }),
      node({ node_key: 'zero', stability_score: 0 }),
      node({ node_key: 'poor', stability_score: 42 })
    ])
    const q = useNodeQuery(rows)
    q.criteria.stabilityBand = 'poor'
    expect(q.pagedNodes.value.map((n) => n.node_key).sort()).toEqual(['poor', 'zero'])
  })

  it('无 stability_score 的节点在启用分档筛选时不命中', () => {
    const rows = ref<UnifiedNode[]>([
      node({ node_key: 'scored', stability_score: 90 }),
      node({ node_key: 'unscored' }) // 无体检记录 -> 无分
    ])
    const q = useNodeQuery(rows)
    q.criteria.stabilityBand = 'good'
    expect(q.pagedNodes.value.map((n) => n.node_key)).toEqual(['scored'])
  })

  it('不启用分档筛选时全部保留', () => {
    const rows = ref<UnifiedNode[]>([
      node({ node_key: 'a', stability_score: 90 }),
      node({ node_key: 'b' })
    ])
    const q = useNodeQuery(rows)
    expect(q.total.value).toBe(2)
  })
})
