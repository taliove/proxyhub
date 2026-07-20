<template>
  <div class="dist-tab">
    <div class="pane-header">
      <el-button type="primary" @click="openCreate">新建分发节点</el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      class="pane-alert"
      title="分发节点通过负载均衡将流量分配到多个上游节点,提供高可用性和流量分发能力。"
    />

    <el-table v-loading="loading" :data="nodes" row-key="id" @row-click="openDetail">
      <el-table-column label="名称" min-width="160">
        <template #default="{ row }">
          <span class="name-cell">
            <span class="name-icon">🔄</span>
            <span>{{ row.name }}</span>
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="path" label="分发路径" min-width="140" show-overflow-tooltip />
      <el-table-column label="负载策略" width="120">
        <template #default="{ row }">{{ lbStrategyLabel(row.lb_strategy) }}</template>
      </el-table-column>
      <el-table-column label="上游数量" width="100">
        <template #default="{ row }">{{ row.upstream_node_keys?.length || 0 }}</template>
      </el-table-column>
      <el-table-column label="流量统计" width="150">
        <template #default="{ row }">
          <div class="traffic-cell">
            <div>↓ {{ formatBytes(row.total_download) }}</div>
            <div>↑ {{ formatBytes(row.total_upload) }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="连接数" width="100">
        <template #default="{ row }">{{ row.total_connections || 0 }}</template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
            {{ row.enabled ? '启用' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <!-- 行内操作 ≤3:编辑 / 启停 / 删除;点击行打开详情抽屉 -->
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <span class="row-ops" @click.stop>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link @click="toggleNode(row)">{{ row.enabled ? '禁用' : '启用' }}</el-button>
            <el-button link type="danger" @click="deleteNode(row)">删除</el-button>
          </span>
        </template>
      </el-table-column>
    </el-table>

    <DistNodeFormDialog
      v-model="dialogVisible"
      v-model:form="form"
      :edit-mode="editMode"
      :submitting="submitting"
      :loading-nodes="loadingNodes"
      :grouped-nodes="groupedNodes"
      @submit="submitForm"
    />
    <DistNodeDetailDrawer
      v-model="detailVisible"
      :node="detailNode"
      :upstream-of="upstreamNodesDisplay"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { DistributionNode, CreateDistributionNodeRequest } from '@/api/distribution-nodes'
import DistNodeFormDialog from './DistNodeFormDialog.vue'
import DistNodeDetailDrawer from './DistNodeDetailDrawer.vue'
import { useDistributionNodes } from '../composables/useDistributionNodes'
import { formatBytes, lbStrategyLabel } from '../distribution-node-utils'

const {
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
} = useDistributionNodes()

const emptyForm = (): CreateDistributionNodeRequest => ({
  name: '',
  path: '',
  upstream_node_keys: [],
  lb_strategy: 'random',
  enabled: true
})

// 表单态与对话框可见性
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const form = ref<CreateDistributionNodeRequest>(emptyForm())
const submitting = ref(false)

const openCreate = async () => {
  editMode.value = false
  editingId.value = null
  form.value = emptyForm()
  await loadAllNodes()
  dialogVisible.value = true
}

const openEdit = async (row: DistributionNode) => {
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
  if (submitting.value) return
  submitting.value = true
  try {
    const ok = await save(form.value, editMode.value ? editingId.value : null)
    if (ok) dialogVisible.value = false
  } finally {
    submitting.value = false
  }
}

// 详情抽屉:替代原展开行;打开前确保全量节点已加载,以还原上游明细
const detailVisible = ref(false)
const detailNode = ref<DistributionNode | null>(null)
const openDetail = async (row: DistributionNode) => {
  detailNode.value = row
  detailVisible.value = true
  await loadAllNodes()
}

onMounted(load)
</script>

<style scoped>
.pane-header {
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--ph-space-3);
}
.pane-alert {
  margin-bottom: var(--ph-space-3);
}
.name-cell {
  display: flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.name-icon {
  font-size: var(--ph-text-md);
}
.traffic-cell {
  font-size: var(--ph-text-xs);
}
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
</style>
