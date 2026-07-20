<template>
  <div class="self-tab">
    <div class="pane-header">
      <el-button @click="openImport">一键导入</el-button>
      <el-button type="primary" @click="openAdd">添加自建节点</el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      class="pane-alert"
      title="自建节点作为 FailBack 常驻注入订阅,豁免关键词/白名单/屏蔽等所有过滤。"
    />

    <el-table v-loading="loading" :data="nodes">
      <el-table-column prop="name" label="名称" show-overflow-tooltip />
      <el-table-column prop="protocol" label="协议" width="90" />
      <el-table-column prop="server" label="服务器" show-overflow-tooltip />
      <el-table-column prop="port" label="端口" width="80" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
            {{ row.enabled ? '启用' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <!-- 行内操作 ≤3:编辑 / 启停 / 删除;删除不可逆用 danger,启停可逆用默认 -->
      <el-table-column label="操作" width="200">
        <template #default="{ row }">
          <span class="row-ops">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link @click="toggleNode(row)">{{ row.enabled ? '禁用' : '启用' }}</el-button>
            <el-button link type="danger" @click="deleteNode(row)">删除</el-button>
          </span>
        </template>
      </el-table-column>
      <!-- 测试列独立,收进下拉 -->
      <el-table-column label="测试" width="120" fixed="right">
        <template #default="{ row }">
          <el-dropdown
            size="small"
            trigger="click"
            @command="(mode: string) => onTestCommand(row, mode)"
          >
            <el-button link type="primary" :disabled="testing">
              测试
              <el-icon><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="quick">快测</el-dropdown-item>
                <el-dropdown-item command="real">真实检测</el-dropdown-item>
                <el-dropdown-item command="bandwidth">带宽测试</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
      </el-table-column>
    </el-table>

    <SelfNodeFormDialog
      v-model="dialogVisible"
      v-model:form="form"
      :edit-mode="editMode"
      :submitting="submitting"
      @submit="submitForm"
    />
    <SelfNodeImportDialog v-model="importDialogVisible" @imported="onImported" />
    <BandwidthTestDialog ref="bwDialog" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ArrowDown } from '@element-plus/icons-vue'
import type { SelfNode } from '@/types'
import { useNodeTest } from '@/composables/useNodeTest'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'
import SelfNodeFormDialog from './SelfNodeFormDialog.vue'
import SelfNodeImportDialog from './SelfNodeImportDialog.vue'
import { useSelfNodes } from '../composables/useSelfNodes'
import { emptyForm, type SelfNodeForm } from '../self-node-utils'

const { nodes, loading, load, save, toggleNode, deleteNode } = useSelfNodes()
const { testing, testNode } = useNodeTest()

// 表单态与对话框可见性
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const form = ref<SelfNodeForm>(emptyForm())
const submitting = ref(false)
const importDialogVisible = ref(false)

const openAdd = () => {
  editMode.value = false
  editingId.value = null
  form.value = emptyForm()
  dialogVisible.value = true
}

const openEdit = (row: SelfNode) => {
  editMode.value = true
  editingId.value = row.id
  const { name, protocol, server, port, uuid, password, cipher } = row
  const { alter_id, network, tls, grpc_service_name, enabled } = row
  form.value = {
    name,
    protocol,
    server,
    port,
    uuid,
    password,
    cipher,
    alter_id,
    network,
    tls,
    grpc_service_name,
    enabled
  }
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

const openImport = () => {
  importDialogVisible.value = true
}

// 导入结果填充表单,关闭导入框后以新建模式打开编辑框(延迟避免同时开关)
const onImported = (parsed: Partial<SelfNodeForm>) => {
  form.value = { ...emptyForm(), ...parsed }
  importDialogVisible.value = false
  editMode.value = false
  editingId.value = null
  setTimeout(() => {
    dialogVisible.value = true
  }, 100)
}

// 测试下拉:quick/real 走消息提示,bandwidth 走弹窗
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)
const onTestCommand = (row: SelfNode, cmd: string) => {
  if (cmd === 'bandwidth') {
    bwDialog.value?.open({ self_node_id: row.id }, row.name)
    return
  }
  testNode({ self_node_id: row.id }, cmd as 'quick' | 'real')
}

onMounted(load)
</script>

<style scoped>
.pane-header {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-3);
}
.pane-alert {
  margin-bottom: var(--ph-space-3);
}
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
</style>
