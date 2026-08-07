// issue #83:节点收藏(展示层星标)与自建专区快捷 Tab 的纯函数/组合式测试。
// fixture 全合成(example.com),不触网。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Node } from '@/types'
import { emptyCriteria, filterNodes } from './predicates'
import {
  applyFavoriteOverrides,
  applyQuickTab,
  quickTabOf,
  useNodeFavorites
} from './composables/useNodeFavorites'
import { setNodeFavorite } from '@/api/nodes'
import { SELF_HOSTED } from './utils'

vi.mock('@/api/nodes', () => ({ setNodeFavorite: vi.fn() }))

const node = (over: Partial<Node> = {}): Node => ({
  name: 'n',
  display_name: '',
  type: 'ss',
  server: 'example.com',
  port: 443,
  tls: false,
  region: '',
  source: '机场A',
  latency: 0,
  available: true,
  node_key: 'example.com:443',
  blocked: false,
  stale: false,
  availability_source: 'never',
  ...over
})

describe('predicates favorite 筛选(issue #83)', () => {
  it('favorite=null 不筛选', () => {
    const rows = [
      node({ favorite: true }),
      node({ node_key: 'b:1', favorite: false }),
      node({ node_key: 'c:1' })
    ]
    expect(filterNodes(rows, { ...emptyCriteria(), favorite: null })).toHaveLength(3)
  })

  it('favorite=true 只看已收藏;缺 favorite 字段的行按未收藏处理', () => {
    const rows = [
      node({ favorite: true }),
      node({ node_key: 'b:1', favorite: false }),
      node({ node_key: 'c:1' })
    ]
    const out = filterNodes(rows, { ...emptyCriteria(), favorite: true })
    expect(out.map((n) => n.node_key)).toEqual(['example.com:443'])
  })
})

describe('applyFavoriteOverrides(乐观覆盖)', () => {
  it('覆盖命中键的 favorite,其余行原样;不改入参', () => {
    const rows = [node({ favorite: false }), node({ node_key: 'b:1', favorite: true })]
    const out = applyFavoriteOverrides(rows, { 'example.com:443': true, 'b:1': false })
    expect(out[0].favorite).toBe(true)
    expect(out[1].favorite).toBe(false)
    expect(rows[0].favorite).toBe(false) // 入参未被改
  })

  it('无覆盖时逐行返回原值', () => {
    const rows = [node({ favorite: true })]
    expect(applyFavoriteOverrides(rows, {})[0].favorite).toBe(true)
  })
})

describe('quickTab 快捷 Tab(自建专区/已收藏,issue #83)', () => {
  it('quickTabOf 按 criteria 反推当前 Tab', () => {
    expect(quickTabOf(emptyCriteria())).toBe('all')
    expect(quickTabOf({ ...emptyCriteria(), source: SELF_HOSTED })).toBe('self')
    expect(quickTabOf({ ...emptyCriteria(), favorite: true })).toBe('favorite')
  })

  it('applyQuickTab 产出筛选补丁:self 直达 source=自建,favorite 直达已收藏', () => {
    expect(applyQuickTab('self')).toEqual({ source: SELF_HOSTED, favorite: null })
    expect(applyQuickTab('favorite')).toEqual({ source: '', favorite: true })
    expect(applyQuickTab('all')).toEqual({ source: '', favorite: null })
  })
})

describe('useNodeFavorites(toggle 乐观更新 + 失败回滚)', () => {
  beforeEach(() => vi.clearAllMocks())

  it('toggle 乐观置位并调 API;成功后保留覆盖', async () => {
    vi.mocked(setNodeFavorite).mockResolvedValue({ success: true })
    const { favoriteOverrides, toggleFavorite } = useNodeFavorites()
    const row = node({ favorite: false })
    await toggleFavorite(row)
    expect(setNodeFavorite).toHaveBeenCalledWith('example.com:443', true)
    expect(favoriteOverrides.value['example.com:443']).toBe(true)
  })

  it('已收藏行 toggle 传 false', async () => {
    vi.mocked(setNodeFavorite).mockResolvedValue({ success: true })
    const { favoriteOverrides, toggleFavorite } = useNodeFavorites()
    await toggleFavorite(node({ favorite: true }))
    expect(setNodeFavorite).toHaveBeenCalledWith('example.com:443', false)
    expect(favoriteOverrides.value['example.com:443']).toBe(false)
  })

  it('API 失败回滚覆盖(全局错误 toast 由 client 拦截器负责)', async () => {
    vi.mocked(setNodeFavorite).mockRejectedValue(new Error('boom'))
    const { favoriteOverrides, toggleFavorite } = useNodeFavorites()
    await toggleFavorite(node({ favorite: false }))
    expect(favoriteOverrides.value['example.com:443']).toBeUndefined()
  })

  it('resetOverrides 清空(池重载后以服务端值为准)', async () => {
    vi.mocked(setNodeFavorite).mockResolvedValue({ success: true })
    const { favoriteOverrides, toggleFavorite, resetOverrides } = useNodeFavorites()
    await toggleFavorite(node({}))
    resetOverrides()
    expect(favoriteOverrides.value).toEqual({})
  })
})
