import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
import { isSelfHosted } from '../utils'

// Selection state and batch operations. Self-hosted nodes are exempt from block/unblock
// (see CONTEXT.md), so block operations only apply to selectableSelection (airport nodes).
// Other operations (refresh-names, detect, exam) apply to all selected nodes uniformly.
export function useNodeBatch(reload: () => void) {
  const selection = ref<Node[]>([])

  const onSelectionChange = (rows: Node[]) => {
    selection.value = rows
  }

  // Airport-only selection for block operations
  const selectableSelection = computed(() => selection.value.filter((n) => !isSelfHosted(n)))

  const blockNode = async (row: Node) => {
    await client.post('/nodes/block', { node_key: row.node_key })
    ElMessage.success('已屏蔽,下次生成订阅生效')
    reload()
  }

  const unblockNode = async (row: Node) => {
    await client.post('/nodes/unblock', { node_key: row.node_key })
    ElMessage.success('已取消屏蔽')
    reload()
  }

  const blockSelected = async () => {
    const keys = selectableSelection.value.map((n) => n.node_key)
    const res = await client.post<unknown, { count: number }>('/nodes/batch-block', {
      node_keys: keys
    })
    ElMessage.success(`已屏蔽 ${res.count} 个节点,下次生成订阅生效`)
    reload()
  }

  const unblockSelected = async () => {
    const keys = selectableSelection.value.map((n) => n.node_key)
    const res = await client.post<unknown, { count: number }>('/nodes/batch-unblock', {
      node_keys: keys
    })
    ElMessage.success(`已取消 ${res.count} 个节点屏蔽`)
    reload()
  }

  const refreshNamesSelected = async () => {
    // Refresh names applies to all selected nodes (both airport and self-hosted)
    const keys = selection.value.map((n) => n.node_key)
    if (keys.length === 0) {
      ElMessage.warning('请先选择节点')
      return
    }
    try {
      const res = await client.post<unknown, { updated: number; total: number }>(
        '/nodes/refresh-names',
        {
          node_keys: keys
        }
      )
      ElMessage.success(`已刷新 ${res.updated} 个节点名称`)
      reload()
    } catch {
      ElMessage.error('刷新名称失败')
    }
  }

  const refreshNameOne = async (row: Node) => {
    try {
      const res = await client.post<unknown, { updated: number; total: number }>(
        '/nodes/refresh-names',
        {
          node_keys: [row.node_key]
        }
      )
      if (res.updated > 0) {
        ElMessage.success('已刷新节点名称')
      } else {
        ElMessage.info('节点名称无变化')
      }
      reload()
    } catch {
      ElMessage.error('刷新名称失败')
    }
  }

  return {
    selection,
    selectableSelection,
    onSelectionChange,
    blockNode,
    unblockNode,
    blockSelected,
    unblockSelected,
    refreshNamesSelected,
    refreshNameOne
  }
}
