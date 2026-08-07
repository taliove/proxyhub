import { computed, type Ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
import { isSelfHosted } from '../utils'

// Batch operations on the effective selection (issue #52): the caller passes the effective
// scope (table checkboxes, or all filtered rows when the select-all-filtered scope is active,
// see useSelectAllFiltered). Self-hosted nodes are exempt from block/unblock (see CONTEXT.md),
// so block operations only apply to selectableSelection (airport nodes within the scope).
// Other operations (refresh-names) apply to all nodes in the scope uniformly.
export function useNodeBatch(reload: () => void, effectiveSelection: Ref<Node[]>) {
  // Airport-only subset of the effective scope, for block operations
  const selectableSelection = computed(() =>
    effectiveSelection.value.filter((n) => !isSelfHosted(n))
  )

  const blockNode = async (row: Node) => {
    await client.post('/nodes/block', { node_key: row.node_key })
    ElMessage.success('已屏蔽，下次生成订阅生效')
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
    ElMessage.success(`已屏蔽 ${res.count} 个节点，下次生成订阅生效`)
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
    // Refresh names applies to all nodes in scope (both airport and self-hosted)
    const keys = effectiveSelection.value.map((n) => n.node_key)
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
    selectableSelection,
    blockNode,
    unblockNode,
    blockSelected,
    unblockSelected,
    refreshNamesSelected,
    refreshNameOne
  }
}
