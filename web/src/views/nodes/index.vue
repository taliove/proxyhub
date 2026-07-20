<template>
  <el-card>
    <template #header>
      <div class="card-header">
        <span>节点管理</span>
        <div class="header-actions">
          <span v-if="lastUpdate" class="muted">最后更新:{{ formatTime(lastUpdate) }}</span>
          <el-button @click="openImport">一键导入</el-button>
          <el-button type="primary" @click="openAddSelf">添加自建节点</el-button>
          <NodeGlobalActions :detecting="detecting" @command="onGlobalCommand" />
        </div>
      </div>
    </template>

    <NodeFilterBar
      v-model:source="criteria.source"
      v-model:region="criteria.region"
      v-model:keyword="criteria.keyword"
      v-model:type="criteria.type"
      v-model:available="criteria.available"
      v-model:blocked="criteria.blocked"
      v-model:stale="criteria.stale"
      v-model:tags="criteria.tags"
      v-model:unlock="criteria.unlock"
      :regions="regions"
      :sources="airportSources"
      :tag-options="tagOptions"
      :unlock-targets="unlockTargets"
      :detecting="detecting"
      @cancel-detect="cancelDetection"
    />

    <NodeBatchBar
      v-if="selectableSelection.length > 0"
      :count="selectableSelection.length"
      :detecting="detecting"
      @block="blockSelected"
      @unblock="unblockSelected"
      @detect="detectSelected"
    />

    <NodeTable
      :nodes="pagedNodes"
      :loading="loading"
      :testing="testing"
      :page="pagination.page"
      :page-size="pagination.pageSize"
      :total="total"
      :exam-summaries="examSummaries"
      @selection-change="onSelectionChange"
      @sort-change="onSortChange"
      @page-change="setPage"
      @size-change="setPageSize"
      @view="openDetail"
      @edit-override="openOverride"
      @edit-self="openEditSelf"
      @toggle-self="onToggleSelf"
      @delete-self="onDeleteSelf"
      @block="blockNode"
      @unblock="unblockNode"
      @test="runTest"
    />

    <NodeDetailDrawer
      v-model="detailVisible"
      :node="detailNode"
      :detecting="detecting"
      @detect="detectOne"
      @exam="openExam"
    />
    <NodeOverrideDialog ref="overrideDialog" :regions="regions" @saved="reload" />
    <SourceBlockDialog ref="sourceBlockDialog" :sources="airportSources" @done="reload" />
    <CleanupDialog
      ref="cleanupDialog"
      :detecting="detecting"
      :trigger-detection="triggerCleanupDetection"
      @done="reload"
    />
    <SelfNodeFormDialog
      v-model="selfDialogVisible"
      v-model:form="selfForm"
      :edit-mode="selfEditMode"
      :submitting="selfSubmitting"
      @submit="submitSelfForm"
    />
    <SelfNodeImportDialog v-model="importDialogVisible" @imported="onImported" />
    <BandwidthTestDialog ref="bwDialog" />
    <NodeExamDialog ref="examDialog" />
  </el-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useNodeTest } from '@/composables/useNodeTest'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'
import NodeExamDialog from '@/components/NodeExamDialog.vue'
import NodeFilterBar from './components/NodeFilterBar.vue'
import NodeGlobalActions from './components/NodeGlobalActions.vue'
import NodeBatchBar from './components/NodeBatchBar.vue'
import NodeTable, { type TestCommand } from './components/NodeTable.vue'
import NodeDetailDrawer from './components/NodeDetailDrawer.vue'
import NodeOverrideDialog from './components/NodeOverrideDialog.vue'
import SourceBlockDialog from './components/SourceBlockDialog.vue'
import CleanupDialog from './components/CleanupDialog.vue'
import SelfNodeFormDialog from './components/SelfNodeFormDialog.vue'
import SelfNodeImportDialog from './components/SelfNodeImportDialog.vue'
import { useNodePool } from './composables/useNodePool'
import { useNodeQuery } from './composables/useNodeQuery'
import { useNodeDetection, type DetectionScope } from './composables/useNodeDetection'
import { useNodeBatch } from './composables/useNodeBatch'
import { useSelfNodes } from './composables/useSelfNodes'
import { useExamSummaries } from './composables/useExamSummaries'
import { buildUnifiedRows, selfNodeIndex, type UnifiedNode } from './selfmerge'
import { tagsOf, unlockTargetsOf } from './nodecells'
import { emptyForm, type SelfNodeForm } from './self-node-utils'
import { formatTime, SELF_HOSTED } from './utils'

const router = useRouter()
const route = useRoute()

// 数据源:节点池(机场 + 已启用自建)+ 自建全量(含禁用)。
const {
  nodes: poolNodes,
  regions,
  airportSources,
  loading,
  lastUpdate,
  load: loadPool,
  loadRegions,
  loadAirportSources
} = useNodePool()
const {
  nodes: selfNodes,
  load: loadSelf,
  save: saveSelf,
  toggleNode: toggleSelf,
  deleteNode: deleteSelf
} = useSelfNodes()

const reload = async () => {
  await Promise.all([loadPool(), loadSelf()])
}

// 合成统一行:池自建行补 id/enabled;禁用自建(不在池中)补进表格(否则无法再管理)。
const unifiedRows = computed<UnifiedNode[]>(() =>
  buildUnifiedRows(poolNodes.value, selfNodes.value)
)
const selfIndex = computed(() => selfNodeIndex(selfNodes.value))

// 筛选下拉的可选项来自当前行集(自动发现标签/解锁目标)。
const tagOptions = computed(() => tagsOf(unifiedRows.value))
const unlockTargets = computed(() => unlockTargetsOf(unifiedRows.value))

// 客户端筛选/排序/分页(谓词见 predicates.ts)。
const {
  criteria,
  pagination,
  total,
  pagedNodes,
  filteredKeys,
  onSortChange,
  setPage,
  setPageSize
} = useNodeQuery(unifiedRows)

// 可见页节点的体检派生摘要(稳定性/出网/体检时间)。
const { summaries: examSummaries, reload: reloadExam } = useExamSummaries(pagedNodes)

const { running: detecting, trigger, cancel } = useNodeDetection()
const {
  selectableSelection,
  onSelectionChange,
  blockNode,
  unblockNode,
  blockSelected,
  unblockSelected
} = useNodeBatch(reload)
const { testing, testNode } = useNodeTest()

// 解锁检测:全部 / 筛选结果 / 选中 / 单节点。检测完成后刷新数据与体检摘要。
const detect = (scope: DetectionScope) =>
  trigger(scope, async () => {
    await reload()
    reloadExam()
  })
const detectSelected = () =>
  detect({ type: 'selected', node_keys: selectableSelection.value.map((n) => n.node_key) })
const detectOne = (node: UnifiedNode) => detect({ type: 'selected', node_keys: [node.node_key] })
const cancelDetection = () => cancel(reload)
const triggerCleanupDetection = (onComplete: () => void) =>
  trigger({ type: 'all' }, onComplete, '检测已启动,完成后自动刷新失败列表')

// 全局操作
const sourceBlockDialog = ref<InstanceType<typeof SourceBlockDialog> | null>(null)
const cleanupDialog = ref<InstanceType<typeof CleanupDialog> | null>(null)
const onGlobalCommand = (cmd: string) => {
  if (cmd === 'detect-all') detect({ type: 'all' })
  else if (cmd === 'detect-filtered') detect({ type: 'selected', node_keys: filteredKeys.value })
  else if (cmd === 'block-source') sourceBlockDialog.value?.open()
  else if (cmd === 'cleanup') cleanupDialog.value?.open()
}

// 详情抽屉 / 覆盖编辑
const detailVisible = ref(false)
const detailNode = ref<UnifiedNode | null>(null)
const openDetail = (row: UnifiedNode) => {
  detailNode.value = row
  detailVisible.value = true
}
const overrideDialog = ref<InstanceType<typeof NodeOverrideDialog> | null>(null)
const openOverride = (row: UnifiedNode) => overrideDialog.value?.open(row)

// 单节点测试:自建按 self_node_id、机场按 node_key(与后端 handleTestNode 一致)。
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)
const examDialog = ref<InstanceType<typeof NodeExamDialog> | null>(null)
const testTarget = (row: UnifiedNode) =>
  row.self_node_id != null ? { self_node_id: row.self_node_id } : { node_key: row.node_key }
const openExam = (node: UnifiedNode) =>
  examDialog.value?.open(testTarget(node), node.display_name || node.name)
const runTest = async (row: UnifiedNode, mode: TestCommand) => {
  const label = row.display_name || row.name
  const target = testTarget(row)
  if (mode === 'bandwidth') {
    bwDialog.value?.open(target, label)
    return
  }
  if (mode === 'exam') {
    examDialog.value?.open(target, label)
    return
  }
  const res = await testNode(target, mode)
  if (res) await loadPool()
}

// 自建节点管理:添加 / 导入 / 编辑 / 启停 / 删除。
const selfDialogVisible = ref(false)
const selfEditMode = ref(false)
const selfEditingId = ref<number | null>(null)
const selfForm = ref<SelfNodeForm>(emptyForm())
const selfSubmitting = ref(false)
const importDialogVisible = ref(false)

const openAddSelf = () => {
  selfEditMode.value = false
  selfEditingId.value = null
  selfForm.value = emptyForm()
  selfDialogVisible.value = true
}
const openEditSelf = (row: UnifiedNode) => {
  if (row.self_node_id == null) return
  const sn = selfIndex.value.get(row.self_node_id)
  if (!sn) return
  const { name, protocol, server, port, uuid, password, cipher } = sn
  const { alter_id, network, tls, grpc_service_name, enabled } = sn
  selfEditMode.value = true
  selfEditingId.value = sn.id
  selfForm.value = {
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
  selfDialogVisible.value = true
}
const submitSelfForm = async () => {
  if (selfSubmitting.value) return
  selfSubmitting.value = true
  try {
    const ok = await saveSelf(selfForm.value, selfEditMode.value ? selfEditingId.value : null)
    if (ok) {
      selfDialogVisible.value = false
      await loadPool()
    }
  } finally {
    selfSubmitting.value = false
  }
}
const openImport = () => {
  importDialogVisible.value = true
}
// 导入结果填充表单,关闭导入框后以新建模式打开编辑框(延迟避免同时开关)
const onImported = (parsed: Partial<SelfNodeForm>) => {
  selfForm.value = { ...emptyForm(), ...parsed }
  importDialogVisible.value = false
  selfEditMode.value = false
  selfEditingId.value = null
  setTimeout(() => {
    selfDialogVisible.value = true
  }, 100)
}
const onToggleSelf = async (row: UnifiedNode) => {
  if (row.self_node_id == null) return
  const sn = selfIndex.value.get(row.self_node_id)
  if (!sn) return
  await toggleSelf(sn)
  await loadPool()
}
const onDeleteSelf = async (row: UnifiedNode) => {
  if (row.self_node_id == null) return
  const sn = selfIndex.value.get(row.self_node_id)
  if (!sn) return
  await deleteSelf(sn)
  await loadPool()
}

// 深链:?source=<来源> 预选来源筛选;兼容旧的 ?tab=self -> 自建。
onMounted(async () => {
  const src = route.query.source
  if (typeof src === 'string' && src) criteria.source = src
  else if (route.query.tab === 'self') criteria.source = SELF_HOSTED
  if (route.query.tab) router.replace({ query: { ...route.query, tab: undefined } })

  loadRegions()
  loadAirportSources()
  await reload()
})
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ph-space-3);
}
.header-actions {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
