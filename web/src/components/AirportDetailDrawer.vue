<template>
  <el-drawer v-model="visible" :title="drawerTitle" size="720px">
    <template v-if="airport">
      <!-- 概况段:基础信息 + 轻管理动作(编辑/启停/删除/刷新/测试/二维码)。
           动作全部上抛给 Airport 管理页既有处理函数,抽屉不持有任何变更逻辑。 -->
      <div class="drawer-block">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="名称">{{ airport.name }}</el-descriptions-item>
          <el-descriptions-item label="简称">
            <span v-if="airport.abbr">{{ airport.abbr }}</span>
            <el-tag v-else type="info" size="small">自动</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="订阅 URL">
            <span class="url-cell">
              <span class="url-text">{{ airport.url }}</span>
              <el-button link type="primary" @click="copyUrl">复制</el-button>
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusDot
              :tone="airport.enabled ? 'success' : 'muted'"
              :label="airport.enabled ? '启用' : '禁用'"
              class="state-dot"
            />
            <span>{{ airport.enabled ? '启用' : '禁用' }}</span>
          </el-descriptions-item>
        </el-descriptions>
        <div class="drawer-actions">
          <el-button size="small" type="primary" @click="emit('edit', airport)">编辑</el-button>
          <el-button size="small" @click="emit('toggle', airport)">
            {{ airport.enabled ? '禁用' : '启用' }}
          </el-button>
          <el-button size="small" :loading="refreshing" @click="emit('refresh', airport)">
            刷新
          </el-button>
          <el-button size="small" @click="emit('test', airport)">测试</el-button>
          <el-button size="small" @click="emit('qrcode', airport)">二维码</el-button>
          <el-button size="small" type="danger" @click="emit('delete', airport)">删除</el-button>
        </div>
      </div>

      <!-- 池内节点明细段:该机场在池内节点的可用性/延迟/最近实测时间(纯读取池快照,
           打开抽屉不触发任何测试/检活)。服务端分页 + keyword 搜索(名称/地区),
           不再一次取全。行轻动作限复制分享链接与触发体检;屏蔽等状态变更归节点管理页。 -->
      <div class="drawer-block">
        <div class="drawer-section-title">池内节点明细</div>
        <el-input
          v-model="nodeKeyword"
          placeholder="搜索节点名称 / 地区(码或中文名)"
          clearable
          class="pool-search"
        />
        <el-table v-loading="nodesLoading" :data="nodes" size="small" border>
          <el-table-column label="名称" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ row.display_name || row.name }}</template>
          </el-table-column>
          <el-table-column label="地区" width="72">
            <template #default="{ row }">{{ regionDisplay(row.region) }}</template>
          </el-table-column>
          <el-table-column label="可用性" width="100">
            <template #default="{ row }">
              <StatusDot :tone="healthTone(row)" :label="healthLabel(row)" class="health-dot" />
              <span>{{ healthLabel(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="延迟" width="80">
            <template #default="{ row }">
              <span class="num">{{ latencyText(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="最近实测" width="100">
            <template #default="{ row }">
              <span v-if="row.detection_last_check" class="num">
                {{ lastCheckText(row.detection_last_check) }}
              </span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="130">
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                :disabled="!canGenerateShareLink(row)"
                @click="copyNodeLink(row)"
                >复制链接</el-button
              >
              <el-button link type="primary" @click="openExam(row)">体检</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <span class="muted">{{ poolEmptyText }}</span>
          </template>
        </el-table>
        <div class="pool-pager">
          <el-pagination
            :current-page="nodePage"
            :page-size="NODE_PAGE_SIZE"
            :total="nodeTotal"
            layout="total, prev, pager, next"
            @current-change="onNodePageChange"
          />
        </div>
      </div>
      <!-- 最近测试报告段:展示最近一次 completed run 的报告(查看不产生新 run);
           「重新测试」「测全部」为显式动作,意图上抛由管理页打开运行模式对话框。 -->
      <div ref="reportSectionEl" class="drawer-block">
        <AirportTestReport :runs="testRuns" :loading="runsLoading" @run-test="onRunTest" />
      </div>
    </template>

    <!-- 节点体检:复用节点管理页同一对话框;打开/进行中不影响抽屉本体。 -->
    <NodeExamDialog ref="examDialog" />
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import type { Airport, Node, NodeListParams, NodePage } from '@/types'
import client from '@/api/client'
import StatusDot from '@/components/StatusDot.vue'
import NodeExamDialog from '@/components/NodeExamDialog.vue'
import AirportTestReport from '@/components/AirportTestReport.vue'
import { listTestRuns, type TestRun } from '@/composables/useAirportTest'
import { canGenerateShareLink, copyNodeLink } from '@/composables/useNodeShare'
import { healthLabel, healthTone, latencyText, regionDisplay } from '@/views/nodes/nodecells'
import { parseTimeMs, relativeTimeZh } from '@/components/exam/examhistory'

const visible = defineModel<boolean>({ required: true })

const props = withDefaults(
  defineProps<{
    airport: Airport | null
    // 单机场刷新进行中(由父级轮询任务中心得出),仅驱动刷新按钮 loading
    refreshing?: boolean
  }>(),
  { refreshing: false }
)

const emit = defineEmits<{
  (e: 'edit', airport: Airport): void
  (e: 'toggle', airport: Airport): void
  (e: 'delete', airport: Airport): void
  (e: 'refresh', airport: Airport): void
  (e: 'test', airport: Airport): void
  (e: 'qrcode', airport: Airport): void
  // 报告段显式重跑:full=false 抽样,full=true 测全部
  (e: 'run-test', payload: { airport: Airport; full: boolean }): void
}>()

const drawerTitle = computed(() =>
  props.airport ? `机场详情 - ${props.airport.name}` : '机场详情'
)

// 池内节点:服务端分页 + keyword 搜索(与节点管理页同源,/api/nodes?source=)。
// 搜索词变化重置到第 1 页;请求失败/空结果降级为空态,不阻塞抽屉。
const NODE_PAGE_SIZE = 10
const nodes = ref<Node[]>([])
const nodesLoading = ref(false)
const nodeKeyword = ref('')
const nodePage = ref(1)
const nodeTotal = ref(0)

const loadNodes = async () => {
  const source = props.airport?.name
  if (!visible.value || !source) return
  nodesLoading.value = true
  try {
    const params: NodeListParams = { page: nodePage.value, page_size: NODE_PAGE_SIZE, source }
    const kw = nodeKeyword.value.trim()
    if (kw) params.keyword = kw
    const data = await client.get<unknown, NodePage>('/nodes', { params })
    nodes.value = data.nodes || []
    nodeTotal.value = data.total || 0
  } catch {
    nodes.value = []
    nodeTotal.value = 0
  } finally {
    nodesLoading.value = false
  }
}

// keyword 防抖搜索(~300ms):停顿后重置到第 1 页重新查询;防抖窗口内连续输入只发一次。
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(nodeKeyword, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    nodePage.value = 1
    loadNodes()
  }, 300)
})

const onNodePageChange = (p: number) => {
  nodePage.value = p
  loadNodes()
}

// 空态文案:有搜索词时为"无匹配"降级,否则为引导刷新文案
const poolEmptyText = computed(() => {
  const kw = nodeKeyword.value.trim()
  return kw ? `未找到匹配「${kw}」的节点` : '该机场当前在池内无节点,可点上方「刷新」拉取入池。'
})

// 最近测试报告:打开抽屉时拉取一次;重跑完成后由父级调 reloadReport 刷新。
const testRuns = ref<TestRun[]>([])
const runsLoading = ref(false)

const loadRuns = async () => {
  if (!props.airport) return
  runsLoading.value = true
  try {
    testRuns.value = await listTestRuns(props.airport.id)
  } catch {
    testRuns.value = []
  } finally {
    runsLoading.value = false
  }
}

// 打开抽屉 / 切换机场时拉取池快照与测试记录;关闭时清空并重置分页/搜索,避免下次闪现旧机场数据。
// 测试记录是纯读取(GET /test/runs),不产生新 run。
watch(
  () => [visible.value, props.airport?.name] as const,
  ([open, name]) => {
    if (open && name) {
      nodeKeyword.value = ''
      nodePage.value = 1
      loadNodes()
      loadRuns()
    } else if (!open) {
      if (searchTimer) clearTimeout(searchTimer)
      nodes.value = []
      nodeKeyword.value = ''
      nodePage.value = 1
      nodeTotal.value = 0
      testRuns.value = []
    }
  },
  { immediate: true }
)

// 报告段重跑意图上抛(抽样/全量由按钮区分);抽屉不持有运行逻辑。
const onRunTest = (full: boolean) => {
  if (!props.airport) return
  emit('run-test', { airport: props.airport, full })
}

// 报告段锚点:列表点分数打开抽屉后由父级调 focusReport 定位到「最近测试」。
const reportSectionEl = ref<HTMLElement | null>(null)

defineExpose({
  // 重跑完成后刷新报告段数据
  reloadReport: loadRuns,
  // 滚动定位到「最近测试」段(jsdom 无 scrollIntoView,故可选调用)
  focusReport: () => {
    nextTick(() => {
      reportSectionEl.value?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
    })
  }
})

// 最近实测时间:表格内用相对时间(与机场列表「最近测试」列同一呈现手法)。
const lastCheckText = (iso: string): string => relativeTimeZh(parseTimeMs(iso))

const copyUrl = async () => {
  if (!props.airport) return
  try {
    await navigator.clipboard.writeText(props.airport.url)
    ElMessage.success('订阅 URL 已复制到剪贴板')
  } catch (err) {
    ElMessage.error(`复制失败: ${err instanceof Error ? err.message : String(err)}`)
  }
}

// 节点体检:机场节点按 node_key(与节点管理页 testTarget 一致)。
const examDialog = ref<InstanceType<typeof NodeExamDialog> | null>(null)
const openExam = (node: Node) => {
  examDialog.value?.open({ node_key: node.node_key }, node.display_name || node.name, node.server)
}
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.drawer-section-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.pool-search {
  margin-bottom: var(--ph-space-2);
}
.pool-pager {
  margin-top: var(--ph-space-2);
  display: flex;
  justify-content: flex-end;
}
.drawer-actions {
  margin-top: var(--ph-space-3);
}
.url-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.url-text {
  word-break: break-all;
}
.state-dot,
.health-dot {
  margin-right: var(--ph-space-1);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
