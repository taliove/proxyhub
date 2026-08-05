<template>
  <div>
    <PageHeader>
      <span v-if="lastUpdate" class="muted">最后更新：{{ formatTime(lastUpdate) }}</span>
      <el-button @click="openImport">一键导入</el-button>
      <el-button type="primary" @click="openAddSelf">添加自建节点</el-button>
    </PageHeader>

    <el-card>
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
        v-model:stability-band="criteria.stabilityBand"
        :regions="regions"
        :sources="airportSources"
        :tag-options="tagOptions"
        :unlock-targets="unlockTargets"
        :detecting="detecting"
        @cancel-detect="cancelDetection"
      />

      <NodeBatchBar
        v-if="effectiveSelection.length > 0"
        :count="effectiveSelection.length"
        :blockable-count="selectableSelection.length"
        :actions="batchActions"
        @block="blockSelected"
        @unblock="unblockSelected"
        @refresh-names="refreshNamesSelected"
        @start="onBatchStart"
        @cancel="onBatchCancel"
        @more-command="onMoreCommand"
      />

      <!-- Gmail 式提示条(issue #52):选中全部筛选结果入口;翻页不清除,改筛选条件自动退出 -->
      <SelectAllBar
        v-if="selectAllPromptVisible"
        :all-filtered="allFilteredSelected"
        :page-count="selection.length"
        :total="total"
        @enter="enterAllFiltered"
        @exit="exitAllFiltered"
      />

      <NodeTable
        :nodes="pagedNodes"
        :loading="loading"
        :detecting="detecting"
        :page="pagination.page"
        :page-size="pagination.pageSize"
        :total="total"
        :exam-summaries="examSummaries"
        :running-exam-keys="runningExamKeys"
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
        @refresh-name="refreshNameOne"
        @test="runTest"
        @copy-link="copyNodeShareLink"
        @show-qr="showNodeQR"
      />

      <NodeDetailDrawer
        v-model="detailVisible"
        :node="detailNode"
        :detecting="detecting"
        :running-exam-keys="runningExamKeys"
        @action="onNodeAction"
      />
      <NodeOverrideDialog ref="overrideDialog" :regions="regions" @saved="reload" />
      <SourceBlockDialog ref="sourceBlockDialog" :sources="airportSources" @done="reload" />
      <CleanupDialog
        ref="cleanupDialog"
        :detecting="detecting"
        :trigger-detection="triggerCleanupDetection"
        @done="reload"
      />
      <PurgeAirportDialog ref="purgeDialog" @done="reload" />
      <SelfNodeFormDialog
        v-model="selfDialogVisible"
        v-model:form="selfForm"
        :edit-mode="selfEditMode"
        :submitting="selfSubmitting"
        @submit="submitSelfForm"
      />
      <SelfNodeImportDialog v-model="importDialogVisible" @imported="onImported" />
      <BandwidthTestDialog ref="bwDialog" />
      <NodeStabilityDialog ref="stabilityDialog" />
      <NodeExamDialog ref="examDialog" />
      <QRCodeDialog
        ref="qrDialog"
        v-model="qrDialogVisible"
        title="节点分享二维码"
        hint="使用客户端扫码即可导入此节点"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'
import NodeStabilityDialog from '@/components/NodeStabilityDialog.vue'
import NodeExamDialog from '@/components/NodeExamDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import NodeFilterBar from './components/NodeFilterBar.vue'
import NodeBatchBar from './components/NodeBatchBar.vue'
import SelectAllBar from './components/SelectAllBar.vue'
import NodeTable from './components/NodeTable.vue'
import type { TestCommand } from './components/node-table-utils'
import NodeDetailDrawer from './components/NodeDetailDrawer.vue'
import NodeOverrideDialog from './components/NodeOverrideDialog.vue'
import SourceBlockDialog from './components/SourceBlockDialog.vue'
import CleanupDialog from './components/CleanupDialog.vue'
import PurgeAirportDialog from './components/PurgeAirportDialog.vue'
import SelfNodeFormDialog from './components/SelfNodeFormDialog.vue'
import SelfNodeImportDialog from './components/SelfNodeImportDialog.vue'
import { useNodePool } from './composables/useNodePool'
import { useNodeQuery } from './composables/useNodeQuery'
import { useNodeUrlState } from './composables/useNodeUrlState'
import { useNodeBatch } from './composables/useNodeBatch'
import { useNodeBatchActions } from './composables/useNodeBatchActions'
import { useSelectAllFiltered } from './composables/useSelectAllFiltered'
import { useSelfNodeForm } from './composables/useSelfNodeForm'
import { useSelfNodes } from './composables/useSelfNodes'
import { useExamSummaries } from './composables/useExamSummaries'
import { useRunningExams } from './composables/useRunningExams'
import { buildUnifiedRows, selfNodeIndex, type UnifiedNode } from './selfmerge'
import { tagsOf, unlockTargetsOf } from './nodecells'
import { formatTime, SELF_HOSTED } from './utils'
import { copyNodeLink, getNodeShareLink } from '@/composables/useNodeShare'

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
const { criteria, pagination, total, filtered, pagedNodes, onSortChange, setPage, setPageSize } =
  useNodeQuery(unifiedRows)

// 跨页"选中全部筛选结果"作用域(issue #52):selection 是表格勾选行(当前页子集),
// effectiveSelection 是批量操作的唯一作用域(全集口径或勾选口径)。
const {
  selection,
  allFiltered: allFilteredSelected,
  promptVisible: selectAllPromptVisible,
  effectiveSelection,
  onSelectionChange,
  enter: enterAllFiltered,
  exit: exitAllFiltered
} = useSelectAllFiltered({ filtered, pagedNodes, criteria })

// 可见页节点的体检派生摘要(稳定性/出网/体检时间)。
const { summaries: examSummaries, reload: reloadExam } = useExamSummaries(pagedNodes)

const {
  selectableSelection,
  blockNode,
  unblockNode,
  blockSelected,
  unblockSelected,
  refreshNamesSelected,
  refreshNameOne
} = useNodeBatch(reload, effectiveSelection)

// 4 个检查动作(出网快速检测 / 出网+稳定性 / 快速测速 / 深度体检)的统一编排:
// 单节点(detectOne)与批量(onBatchStart/onBatchCancel/batchActions)共用同一套启动器。
const {
  detecting,
  detectOne,
  cancelDetection,
  triggerCleanupDetection,
  batchActions,
  onBatchStart,
  onBatchCancel
} = useNodeBatchActions(effectiveSelection, reload, reloadExam)

// 进行中的 exam 任务:轮询任务中心,提取 kind=exam + status=running 的 key 集合。
// 用于在节点行显示"查看进度"而非"深度体检"按钮。
const { runningExamKeys } = useRunningExams()

// 页面级非检查命令(批量栏「更多」菜单):按机场屏蔽 / 清空机场节点 / 清理失败节点。
const sourceBlockDialog = ref<InstanceType<typeof SourceBlockDialog> | null>(null)
const cleanupDialog = ref<InstanceType<typeof CleanupDialog> | null>(null)
const purgeDialog = ref<InstanceType<typeof PurgeAirportDialog> | null>(null)
const onMoreCommand = (cmd: string) => {
  if (cmd === 'block-source') sourceBlockDialog.value?.open()
  else if (cmd === 'purge-airport') purgeDialog.value?.open()
  else if (cmd === 'cleanup') cleanupDialog.value?.open()
}

// 详情抽屉 / 覆盖编辑
const detailVisible = ref(false)
const detailNode = ref<UnifiedNode | null>(null)
const detailNodeKey = ref<string | null>(null)
const openDetail = (row: UnifiedNode) => {
  detailNode.value = row
  detailNodeKey.value = row.node_key
  detailVisible.value = true
}
// 详情抽屉关闭时清空 key(触发 URL 同步)
watch(detailVisible, (visible) => {
  if (!visible) detailNodeKey.value = null
})
const overrideDialog = ref<InstanceType<typeof NodeOverrideDialog> | null>(null)
const openOverride = (row: UnifiedNode) => overrideDialog.value?.open(row)

// 单节点检查:自建按 self_node_id、机场按 node_key(与后端 handleTestNode/resolveTestNode 一致)。
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)
const stabilityDialog = ref<InstanceType<typeof NodeStabilityDialog> | null>(null)
const examDialog = ref<InstanceType<typeof NodeExamDialog> | null>(null)
const qrDialog = ref<InstanceType<typeof QRCodeDialog> | null>(null)
const qrDialogVisible = ref(false)

const testTarget = (row: UnifiedNode) =>
  row.self_node_id != null ? { self_node_id: row.self_node_id } : { node_key: row.node_key }
// 本机实测入口:带 node_key query 跳独立页预填标注(浏览器端验收测量,非服务端检测)
const goSpeedtest = (row: UnifiedNode) =>
  router.push({ path: '/speedtest', query: { node_key: row.node_key } })

// 行内/抽屉单节点检查:4 动作与批量面同名同义(见 CONTEXT「检查动作」)。
//   detect    → 全语义任务口径:与批量同走 batch_detection(含解锁落库 + 重算标签),
//               进行中复用全局 detecting 态(行内按钮标注/禁用)。
//   stability → 出网+稳定性 SSE 弹框。
//   speedtest → 快速测速 SSE 弹框(必须带 mode=speedtest 走基准口径,见 issue 0031 实现注)。
//   exam      → 深度体检 SSE 弹框(进行中自动附加,回放+续传)。
// client-speedtest → 本机实测(跳独立页)。
const runNodeAction = (row: UnifiedNode, cmd: TestCommand) => {
  const label = row.display_name || row.name
  const target = testTarget(row)
  switch (cmd) {
    case 'detect':
      // 全语义任务口径:与批量共用 batch_detection(单节点即 node_keys 只含本节点)。
      return detectOne(row)
    case 'stability':
      return stabilityDialog.value?.open(target, label)
    case 'speedtest':
      // 快速测速:基准下行 + 上行,弹框沿用带宽流式 UX,mode=speedtest 切基准口径。
      return bwDialog.value?.open(target, label, { mode: 'speedtest' })
    case 'exam':
      return examDialog.value?.open(target, label, row.server)
    case 'client-speedtest':
      return goSpeedtest(row)
  }
}
// 表格行 @test 与抽屉 @action 共用同一分发。
const runTest = runNodeAction
const onNodeAction = runNodeAction

// 自建节点管理:添加 / 导入 / 编辑对话框状态机(启停 / 删除仍在下方,直走 useSelfNodes)。
const {
  selfDialogVisible,
  selfEditMode,
  selfForm,
  selfSubmitting,
  importDialogVisible,
  openAddSelf,
  openEditSelf,
  submitSelfForm,
  openImport,
  onImported
} = useSelfNodeForm({ selfIndex, saveSelf, reloadPool: loadPool })

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

// 节点分享:复制链接与二维码
const copyNodeShareLink = async (node: UnifiedNode) => {
  await copyNodeLink(node)
}
const showNodeQR = async (node: UnifiedNode) => {
  try {
    const uri = await getNodeShareLink(node)
    qrDialog.value?.show(uri)
  } catch {
    // Error already handled by getNodeShareLink via ElMessage
  }
}

// URL 状态同步:筛选条件与详情抽屉双向绑定到 query。
const { restoreFromUrl } = useNodeUrlState(criteria, detailVisible, detailNodeKey)

// 深链:从 URL 恢复筛选条件(替代旧的手动处理);兼容旧的 ?tab=self -> 自建。
onMounted(async () => {
  restoreFromUrl()
  if (route.query.tab === 'self') criteria.source = SELF_HOSTED
  if (route.query.tab) router.replace({ query: { ...route.query, tab: undefined } })

  loadRegions()
  loadAirportSources()
  await reload()

  // URL 带 detail 参数时,数据加载后自动打开对应节点详情
  if (detailNodeKey.value) {
    const targetNode = unifiedRows.value.find((n) => n.node_key === detailNodeKey.value)
    if (targetNode) openDetail(targetNode)
  }
})
</script>

<style scoped>
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
