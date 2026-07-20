<template>
  <div class="airport-tab">
    <!-- 标题行:最后更新 + 全局操作下拉(与选中集无关的页面级操作) -->
    <div class="pane-header">
      <span v-if="lastUpdate" class="muted">最后更新:{{ formatTime(lastUpdate) }}</span>
      <NodeGlobalActions :detecting="detecting" @command="onGlobalCommand" />
    </div>

    <NodeFilterBar
      v-model:region="filters.region"
      v-model:type="filters.type"
      v-model:available="filters.available"
      v-model:blocked="filters.blocked"
      v-model:stale="filters.stale"
      v-model:source="filters.source"
      :regions="regions"
      :detecting="detecting"
      @search="applySourceFilter"
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
      :nodes="nodes"
      :loading="loading"
      :testing="testing"
      :page="pagination.page"
      :page-size="pagination.pageSize"
      :total="pagination.total"
      :exam-summaries="examSummaries"
      @selection-change="onSelectionChange"
      @sort-change="onSortChange"
      @page-change="onPageChange"
      @size-change="onSizeChange"
      @view="openDetail"
      @edit-override="openOverride"
      @edit-self="editSelfNode"
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
    <NodeOverrideDialog ref="overrideDialog" :regions="regions" @saved="load" />
    <SourceBlockDialog ref="sourceBlockDialog" :sources="airportSources" @done="load" />
    <CleanupDialog
      ref="cleanupDialog"
      :detecting="detecting"
      :trigger-detection="triggerCleanupDetection"
      @done="load"
    />
    <BandwidthTestDialog ref="bwDialog" />
    <NodeExamDialog ref="examDialog" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { Node } from '@/types'
import { useNodeTest } from '@/composables/useNodeTest'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'
import NodeExamDialog from '@/components/NodeExamDialog.vue'
import NodeFilterBar from './NodeFilterBar.vue'
import NodeGlobalActions from './NodeGlobalActions.vue'
import NodeBatchBar from './NodeBatchBar.vue'
import NodeTable, { type TestCommand } from './NodeTable.vue'
import NodeDetailDrawer from './NodeDetailDrawer.vue'
import NodeOverrideDialog from './NodeOverrideDialog.vue'
import SourceBlockDialog from './SourceBlockDialog.vue'
import CleanupDialog from './CleanupDialog.vue'
import { useNodeFilters } from '../composables/useNodeFilters'
import { useNodeList } from '../composables/useNodeList'
import { useNodeDetection, type DetectionScope } from '../composables/useNodeDetection'
import { useNodeBatch } from '../composables/useNodeBatch'
import { useExamSummaries } from '../composables/useExamSummaries'
import { formatTime } from '../utils'

const router = useRouter()

const { filters, activeFilterParams } = useNodeFilters(() => {
  pagination.page = 1
  load()
})
const {
  nodes,
  regions,
  airportSources,
  loading,
  lastUpdate,
  pagination,
  load,
  onSortChange,
  loadRegions,
  loadAirportSources
} = useNodeList(activeFilterParams)
const { running: detecting, trigger, cancel } = useNodeDetection()
const {
  selectableSelection,
  onSelectionChange,
  blockNode,
  unblockNode,
  blockSelected,
  unblockSelected
} = useNodeBatch(load)
const { testing, testNode } = useNodeTest()
// 节点行体检摘要徽标:随当前页节点整批拉取“最近一次体检”。
const { summaries: examSummaries } = useExamSummaries(nodes)

// 解锁检测(全部/筛选结果/选中/单节点)
const detect = (scope: DetectionScope) => trigger(scope, load)
const detectSelected = () =>
  detect({ type: 'selected', node_keys: selectableSelection.value.map((n) => n.node_key) })
const detectOne = (node: Node) => detect({ type: 'selected', node_keys: [node.node_key] })
const cancelDetection = () => cancel(load)
const triggerCleanupDetection = (onComplete: () => void) =>
  trigger({ type: 'all' }, onComplete, '检测已启动,完成后自动刷新失败列表')

// 筛选与分页
const applySourceFilter = () => {
  pagination.page = 1
  load()
}
const onPageChange = (p: number) => {
  pagination.page = p
  load()
}
const onSizeChange = (s: number) => {
  pagination.pageSize = s
  pagination.page = 1
  load()
}

// 全局操作下拉
const sourceBlockDialog = ref<InstanceType<typeof SourceBlockDialog> | null>(null)
const cleanupDialog = ref<InstanceType<typeof CleanupDialog> | null>(null)
const onGlobalCommand = (cmd: string) => {
  if (cmd === 'detect-all') detect({ type: 'all' })
  else if (cmd === 'detect-filtered') detect({ type: 'query', query: activeFilterParams() })
  else if (cmd === 'block-source') sourceBlockDialog.value?.open()
  else if (cmd === 'cleanup') cleanupDialog.value?.open()
}

// 详情抽屉 / 覆盖编辑 / 单节点测试
const detailVisible = ref(false)
const detailNode = ref<Node | null>(null)
const openDetail = (row: Node) => {
  detailNode.value = row
  detailVisible.value = true
}
// 抽屉空历史引导 -> 直接对该节点开跑深度体检
const openExam = (node: Node) =>
  examDialog.value?.open({ node_key: node.node_key }, node.display_name || node.name)
const overrideDialog = ref<InstanceType<typeof NodeOverrideDialog> | null>(null)
const openOverride = (row: Node) => overrideDialog.value?.open(row)
const editSelfNode = () => router.push({ path: '/nodes', query: { tab: 'self' } })
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)
const examDialog = ref<InstanceType<typeof NodeExamDialog> | null>(null)
const runTest = async (row: Node, mode: TestCommand) => {
  const label = row.display_name || row.name
  if (mode === 'bandwidth') {
    bwDialog.value?.open({ node_key: row.node_key }, label)
    return
  }
  if (mode === 'exam') {
    examDialog.value?.open({ node_key: row.node_key }, label)
    return
  }
  const res = await testNode({ node_key: row.node_key }, mode)
  if (res) load()
}

onMounted(() => {
  loadRegions()
  loadAirportSources()
  load()
})
</script>

<style scoped>
.pane-header {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-3);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
