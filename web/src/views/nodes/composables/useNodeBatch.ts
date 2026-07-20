import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
import { isSelfHosted } from '../utils'

// 选中态与屏蔽操作:单个节点 / 选中集。自建节点豁免屏蔽(见 CONTEXT.md),
// 因此批量操作一律只作用于 selectableSelection。
export function useNodeBatch(reload: () => void) {
  const selection = ref<Node[]>([])

  const onSelectionChange = (rows: Node[]) => {
    selection.value = rows
  }

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

  return {
    selection,
    selectableSelection,
    onSelectionChange,
    blockNode,
    unblockNode,
    blockSelected,
    unblockSelected
  }
}
