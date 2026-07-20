import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
import { apiErrorMessage } from '../utils'
import {
  listDistributionNodes,
  createDistributionNode,
  updateDistributionNode,
  deleteDistributionNode,
  toggleDistributionNode,
  type DistributionNode,
  type CreateDistributionNodeRequest,
  type UpdateDistributionNodeRequest
} from '@/api/distribution-nodes'

// 分发节点数据获取与增删改,外加上游节点选择所需的全量节点列表与分组。
export function useDistributionNodes() {
  const nodes = ref<DistributionNode[]>([])
  const allNodes = ref<Node[]>([])
  const loading = ref(false)
  const loadingNodes = ref(false)

  const load = async () => {
    loading.value = true
    try {
      nodes.value = await listDistributionNodes()
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '加载失败'))
    } finally {
      loading.value = false
    }
  }

  // 上游选择需要全量节点(不分页)
  const loadAllNodes = async () => {
    loadingNodes.value = true
    try {
      const data = await client.get<unknown, { nodes: Node[] }>('/nodes', {
        params: { page_size: 10000, page: 1 }
      })
      allNodes.value = data.nodes || []
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '加载节点列表失败'))
    } finally {
      loadingNodes.value = false
    }
  }

  // 上游节点按来源分组("自建"置顶,机场按名称排序,排除已有分发节点)
  const groupedNodes = computed(() => {
    const bySource = new Map<string, Node[]>()
    allNodes.value
      .filter((n) => n.source !== 'distribution')
      .forEach((node) => {
        const source = node.source === 'self-hosted' ? '自建' : node.source
        if (!bySource.has(source)) bySource.set(source, [])
        bySource.get(source)!.push(node)
      })

    const sortedSources = Array.from(bySource.keys()).sort((a, b) => {
      if (a === '自建') return -1
      if (b === '自建') return 1
      return a.localeCompare(b)
    })

    return sortedSources.map((label) => ({ label, nodes: bySource.get(label)! }))
  })

  // 展开上游 node_key 为可读明细(供详情抽屉)
  const upstreamNodesDisplay = (row: DistributionNode) => {
    if (!row.upstream_node_keys || row.upstream_node_keys.length === 0) return []
    return allNodes.value
      .filter((n) => row.upstream_node_keys.includes(n.node_key))
      .map((n) => ({
        name: n.display_name || n.name,
        region: n.region,
        type: n.type,
        source: n.source
      }))
  }

  const save = async (
    form: CreateDistributionNodeRequest,
    editingId: number | null
  ): Promise<boolean> => {
    try {
      if (editingId !== null) {
        await updateDistributionNode(editingId, form as UpdateDistributionNodeRequest)
        ElMessage.success('更新成功')
      } else {
        await createDistributionNode(form)
        ElMessage.success('创建成功')
      }
      await load()
      return true
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '保存失败'))
      return false
    }
  }

  const toggleNode = async (row: DistributionNode) => {
    try {
      const result = await toggleDistributionNode(row.id)
      ElMessage.success(result.enabled ? '已启用' : '已禁用')
      await load()
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '操作失败'))
    }
  }

  const deleteNode = async (row: DistributionNode) => {
    try {
      await ElMessageBox.confirm('确定删除此分发节点?', '确认', { type: 'warning' })
    } catch {
      return
    }
    try {
      await deleteDistributionNode(row.id)
      ElMessage.success('已删除')
      await load()
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '删除失败'))
    }
  }

  return {
    nodes,
    loading,
    loadingNodes,
    groupedNodes,
    load,
    loadAllNodes,
    upstreamNodesDisplay,
    save,
    toggleNode,
    deleteNode
  }
}
