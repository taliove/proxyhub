<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      style="margin-bottom: 12px;"
      title="分发节点通过负载均衡将流量分配到多个上游节点，提供高可用性和流量分发能力。"
    />

    <div style="margin-bottom: 12px;">
      <el-button type="primary" @click="openCreateDialog">新建分发节点</el-button>
    </div>

    <el-table :data="nodes" v-loading="loading" row-key="id">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div style="padding: 12px 48px;">
            <div style="margin-bottom: 8px;">
              <strong>上游节点 ({{ row.upstream_node_keys?.length || 0 }})</strong>
            </div>
            <el-table :data="getUpstreamNodesDisplay(row)" size="small" border style="max-width: 800px;">
              <el-table-column prop="name" label="节点名称" min-width="180" show-overflow-tooltip />
              <el-table-column prop="region" label="地区" width="90" />
              <el-table-column prop="type" label="类型" width="90" />
              <el-table-column prop="source" label="来源" min-width="120" show-overflow-tooltip />
            </el-table>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="名称" min-width="160">
        <template #default="{ row }">
          <span style="display: flex; align-items: center; gap: 6px;">
            <span style="font-size: 16px;">🔄</span>
            <span>{{ row.name }}</span>
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="path" label="分发路径" min-width="140" show-overflow-tooltip />
      <el-table-column label="负载策略" width="120">
        <template #default="{ row }">
          {{ getLBStrategyLabel(row.lb_strategy) }}
        </template>
      </el-table-column>
      <el-table-column label="上游数量" width="100">
        <template #default="{ row }">
          {{ row.upstream_node_keys?.length || 0 }}
        </template>
      </el-table-column>
      <el-table-column label="流量统计" width="150">
        <template #default="{ row }">
          <div style="font-size: 12px;">
            <div>↓ {{ formatBytes(row.total_download) }}</div>
            <div>↑ {{ formatBytes(row.total_upload) }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="连接数" width="100">
        <template #default="{ row }">
          {{ row.total_connections || 0 }}
        </template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
            {{ row.enabled ? '启用' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
          <el-button link @click="toggleNode(row)">{{ row.enabled ? '禁用' : '启用' }}</el-button>
          <el-button link type="danger" @click="deleteNode(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="editMode ? '编辑分发节点' : '新建分发节点'" width="600px">
      <el-form :model="form" label-width="120px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="例如：香港分发" />
        </el-form-item>
        <el-form-item label="分发路径" required>
          <el-input v-model="form.path" placeholder="例如：/hk-dist">
            <template #prepend>
              <el-button @click="autoGeneratePath" :disabled="!form.name">自动生成</el-button>
            </template>
          </el-input>
          <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px;">
            路径必须以 / 开头，用于订阅分发
          </div>
        </el-form-item>
        <el-form-item label="负载均衡策略" required>
          <el-select v-model="form.lb_strategy" style="width: 100%;">
            <el-option label="随机 (random)" value="random" />
            <el-option label="轮询 (round_robin)" value="round_robin" />
            <el-option label="最少连接 (least_conn)" value="least_conn" />
          </el-select>
        </el-form-item>
        <el-form-item label="上游节点" required>
          <el-select
            v-model="form.upstream_node_keys"
            multiple
            filterable
            placeholder="选择上游节点"
            style="width: 100%;"
            :loading="loadingNodes"
          >
            <el-option-group
              v-for="group in groupedNodes"
              :key="group.label"
              :label="group.label"
            >
              <el-option
                v-for="node in group.nodes"
                :key="node.node_key"
                :label="`${node.display_name || node.name} (${node.region})`"
                :value="node.node_key"
                :disabled="!node.available"
              >
                <span>{{ node.display_name || node.name }}</span>
                <span style="color: var(--el-text-color-secondary); margin-left: 8px;">
                  {{ node.region }} · {{ node.type }}
                </span>
              </el-option>
            </el-option-group>
          </el-select>
          <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px;">
            已选择 {{ form.upstream_node_keys.length }} 个节点
          </div>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm" :loading="submitting" :disabled="submitting">
          {{ editMode ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
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

const nodes = ref<DistributionNode[]>([])
const allNodes = ref<Node[]>([])
const loading = ref(false)
const loadingNodes = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const submitting = ref(false)

const emptyForm = (): CreateDistributionNodeRequest => ({
  name: '',
  path: '',
  upstream_node_keys: [],
  lb_strategy: 'random',
  enabled: true
})

const form = ref<CreateDistributionNodeRequest>(emptyForm())

const getLBStrategyLabel = (strategy: string): string => {
  const labels: Record<string, string> = {
    random: '随机',
    round_robin: '轮询',
    least_conn: '最少连接'
  }
  return labels[strategy] || strategy
}

const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// Group nodes by source (airport name / "自建" / exclude distribution)
const groupedNodes = computed(() => {
  const groups: { label: string; nodes: Node[] }[] = []
  const bySource = new Map<string, Node[]>()

  // Filter out existing distribution nodes (they can't be upstream for another distribution)
  const availableNodes = allNodes.value.filter(n => {
    // Exclude nodes that are already distribution nodes
    return n.source !== 'distribution'
  })

  availableNodes.forEach(node => {
    const source = node.source === 'self-hosted' ? '自建' : node.source
    if (!bySource.has(source)) {
      bySource.set(source, [])
    }
    bySource.get(source)!.push(node)
  })

  // Sort groups: "自建" first, then airports alphabetically
  const sortedSources = Array.from(bySource.keys()).sort((a, b) => {
    if (a === '自建') return -1
    if (b === '自建') return 1
    return a.localeCompare(b)
  })

  sortedSources.forEach(source => {
    groups.push({
      label: source,
      nodes: bySource.get(source)!
    })
  })

  return groups
})

// Get upstream nodes display for expanded row
const getUpstreamNodesDisplay = (row: DistributionNode) => {
  if (!row.upstream_node_keys || row.upstream_node_keys.length === 0) {
    return []
  }
  return allNodes.value
    .filter(n => row.upstream_node_keys.includes(n.node_key))
    .map(n => ({
      name: n.display_name || n.name,
      region: n.region,
      type: n.type,
      source: n.source
    }))
}

const autoGeneratePath = () => {
  if (!form.value.name) {
    ElMessage.warning('请先输入名称')
    return
  }
  // Generate path from name: remove spaces, convert to lowercase, add / prefix
  const path = '/' + form.value.name
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
  form.value.path = path
}

const load = async () => {
  loading.value = true
  try {
    nodes.value = await listDistributionNodes()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '加载失败')
  } finally {
    loading.value = false
  }
}

const loadAllNodes = async () => {
  loadingNodes.value = true
  try {
    // Fetch all nodes without pagination
    const data = await client.get<any, { nodes: Node[] }>('/nodes', {
      params: { page_size: 10000, page: 1 }
    })
    allNodes.value = data.nodes || []
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '加载节点列表失败')
  } finally {
    loadingNodes.value = false
  }
}

const openCreateDialog = async () => {
  editMode.value = false
  editingId.value = null
  form.value = emptyForm()
  await loadAllNodes()
  dialogVisible.value = true
}

const openEditDialog = async (row: DistributionNode) => {
  editMode.value = true
  editingId.value = row.id
  form.value = {
    name: row.name,
    path: row.path,
    upstream_node_keys: row.upstream_node_keys || [],
    lb_strategy: row.lb_strategy,
    enabled: row.enabled
  }
  await loadAllNodes()
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!form.value.name.trim()) {
    ElMessage.warning('名称不能为空')
    return
  }
  if (!form.value.path.trim()) {
    ElMessage.warning('分发路径不能为空')
    return
  }
  if (!form.value.path.startsWith('/')) {
    ElMessage.warning('分发路径必须以 / 开头')
    return
  }
  if (form.value.upstream_node_keys.length === 0) {
    ElMessage.warning('请至少选择一个上游节点')
    return
  }
  if (submitting.value) {
    return
  }

  submitting.value = true
  try {
    if (editMode.value && editingId.value !== null) {
      await updateDistributionNode(editingId.value, form.value as UpdateDistributionNodeRequest)
      ElMessage.success('更新成功')
    } else {
      await createDistributionNode(form.value)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '保存失败')
  } finally {
    submitting.value = false
  }
}

const toggleNode = async (row: DistributionNode) => {
  try {
    const result = await toggleDistributionNode(row.id)
    ElMessage.success(result.enabled ? '已启用' : '已禁用')
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '操作失败')
  }
}

const deleteNode = async (row: DistributionNode) => {
  try {
    await ElMessageBox.confirm('确定删除此分发节点？', '确认', { type: 'warning' })
    await deleteDistributionNode(row.id)
    ElMessage.success('已删除')
    load()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error(e?.response?.data || '删除失败')
    }
  }
}

onMounted(load)
</script>
