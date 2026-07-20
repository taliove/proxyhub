import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { SelfNode } from '@/types'
import client from '@/api/client'
import { apiErrorMessage } from '../utils'
import { emptyForm, type SelfNodeForm } from '../self-node-utils'

// 自建节点数据获取与增删改:列表加载、创建/更新、启停切换、删除。
// 表单态与对话框可见性由装配层持有,这里只暴露纯数据操作。
export function useSelfNodes() {
  const nodes = ref<SelfNode[]>([])
  const loading = ref(false)

  const load = async () => {
    loading.value = true
    try {
      const data = await client.get<unknown, { nodes: SelfNode[] }>('/self-nodes')
      nodes.value = data.nodes || []
    } finally {
      loading.value = false
    }
  }

  const save = async (form: SelfNodeForm, editingId: number | null): Promise<boolean> => {
    if (!form.name.trim() || !form.server.trim()) {
      ElMessage.warning('名称和服务器不能为空')
      return false
    }
    try {
      if (editingId !== null) {
        await client.put(`/self-nodes/${editingId}`, form)
        ElMessage.success('更新成功')
      } else {
        await client.post('/self-nodes', form)
        ElMessage.success('添加成功')
      }
      await load()
      return true
    } catch (e) {
      ElMessage.error(apiErrorMessage(e, '保存失败'))
      return false
    }
  }

  const toggleNode = async (row: SelfNode) => {
    await client.post(`/self-nodes/${row.id}/toggle`, { enabled: !row.enabled })
    ElMessage.success(row.enabled ? '已禁用' : '已启用')
    await load()
  }

  const deleteNode = async (row: SelfNode) => {
    try {
      await ElMessageBox.confirm('确定删除此自建节点?', '确认', { type: 'warning' })
    } catch {
      return
    }
    await client.delete(`/self-nodes/${row.id}`)
    ElMessage.success('已删除')
    await load()
  }

  return { nodes, loading, load, save, toggleNode, deleteNode, emptyForm }
}
