// issue #82:行内屏蔽/解除屏蔽的组合函数路径——单节点 block/unblock 调对应接口、
// 提示并刷新列表;自建节点在屏蔽作用域内被豁免(selectableSelection 过滤)。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { useNodeBatch } from './useNodeBatch'
import client from '@/api/client'
import { ElMessage } from 'element-plus'
import { SELF_HOSTED } from '../utils'
import type { Node } from '@/types'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn(), post: vi.fn() }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }
}))

const node = (over: Partial<Node> = {}): Node =>
  ({
    name: 'n',
    display_name: '',
    type: 'vmess',
    server: 'example.com',
    port: 443,
    tls: true,
    region: 'HK',
    source: 'airport-a',
    latency: 100,
    available: true,
    node_key: 'k1',
    blocked: false,
    stale: false,
    ...over
  }) as Node

const setup = (selection: Node[] = []) => {
  const reload = vi.fn()
  const effectiveSelection = ref<Node[]>(selection)
  const batch = useNodeBatch(reload, effectiveSelection)
  return { reload, effectiveSelection, batch }
}

describe('useNodeBatch 行内屏蔽/解除屏蔽（issue #82）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('blockNode:调 /nodes/block 带 node_key,提示后刷新列表', async () => {
    const { reload, batch } = setup()
    vi.mocked(client.post).mockResolvedValue({} as never)

    await batch.blockNode(node({ node_key: 'k-block' }))

    expect(client.post).toHaveBeenCalledWith('/nodes/block', { node_key: 'k-block' })
    expect(ElMessage.success).toHaveBeenCalledWith('已屏蔽，下次生成订阅生效')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('unblockNode:调 /nodes/unblock 带 node_key,提示后刷新列表', async () => {
    const { reload, batch } = setup()
    vi.mocked(client.post).mockResolvedValue({} as never)

    await batch.unblockNode(node({ node_key: 'k-unblock', blocked: true }))

    expect(client.post).toHaveBeenCalledWith('/nodes/unblock', { node_key: 'k-unblock' })
    expect(ElMessage.success).toHaveBeenCalledWith('已取消屏蔽')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('接口失败时不刷新列表（错误向上抛，由调用方/拦截器处理）', async () => {
    const { reload, batch } = setup()
    vi.mocked(client.post).mockRejectedValue(new Error('network') as never)

    await expect(batch.blockNode(node())).rejects.toThrow('network')
    expect(reload).not.toHaveBeenCalled()
  })

  it('selectableSelection:自建节点被豁免，屏蔽作用域只含机场节点', () => {
    const { batch } = setup([
      node({ node_key: 'k-airport' }),
      node({ node_key: 'k-self', source: SELF_HOSTED })
    ])

    expect(batch.selectableSelection.value.map((n) => n.node_key)).toEqual(['k-airport'])
  })
})
