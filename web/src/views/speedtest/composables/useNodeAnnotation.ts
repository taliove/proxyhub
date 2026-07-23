// 标注下拉:节点池选项(含"直连")+ URL query 预填(?node_key=,学 /jobs?id= 模式)。
import { computed, ref } from 'vue'
import client from '@/api/client'
import type { Node, NodePage } from '@/types'

// 标注下拉一次性取全量池(与节点管理页同口径,池规模有界)。
const POOL_PAGE_SIZE = 100000

// NodeOption 标注下拉项:value 为 node_key,'' 是固定的"直连"项。
export interface NodeOption {
  value: string
  label: string
  region: string
}

export const DIRECT_OPTION: NodeOption = { value: '', label: '直连(不经节点)', region: '' }

// PrefillResult URL 预填结果:applied = 命中池中节点;orphan = 节点已不在池;
// none = 无 query(默认直连)。
export type PrefillResult = 'applied' | 'orphan' | 'none'

export function useNodeAnnotation() {
  const nodes = ref<Node[]>([])
  const loading = ref(false)
  // selectedKey 当前标注:'' = 直连
  const selectedKey = ref('')

  const load = async () => {
    loading.value = true
    try {
      const data = await client.get<unknown, NodePage>('/nodes', {
        params: { page: 1, page_size: POOL_PAGE_SIZE }
      })
      nodes.value = data.nodes || []
    } finally {
      loading.value = false
    }
  }

  const options = computed<NodeOption[]>(() => [
    DIRECT_OPTION,
    ...nodes.value.map((n) => ({
      value: n.node_key,
      label: n.display_name || n.name,
      region: n.region
    }))
  ])

  const poolKeys = computed<ReadonlySet<string>>(() => new Set(nodes.value.map((n) => n.node_key)))

  const labelOf = (key: string): string => {
    if (key === '') return '直连'
    const node = nodes.value.find((n) => n.node_key === key)
    return node ? node.display_name || node.name : key
  }

  // applyQuery 用 URL query 预填标注:命中池则选中;不在池不清空选择,返回 orphan
  // 由页面提示(学 /jobs?id= 的"无效 id 提示并回落"模式)。
  const applyQuery = (nodeKey: unknown): PrefillResult => {
    if (typeof nodeKey !== 'string' || nodeKey === '') return 'none'
    if (poolKeys.value.has(nodeKey)) {
      selectedKey.value = nodeKey
      return 'applied'
    }
    return 'orphan'
  }

  return { nodes, loading, selectedKey, options, poolKeys, labelOf, load, applyQuery }
}
