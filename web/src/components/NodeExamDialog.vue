<template>
  <el-dialog
    v-model="visible"
    width="960px"
    class="exam-dialog"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    :show-close="true"
    @close="onClose"
    @closed="onClosed"
  >
    <!-- 头部:标题 + 状态灯 + 三态标签(连接中/体检中/重连中/完成/已取消/连接失败)。 -->
    <template #header>
      <div class="exam-dialog-head">
        <span class="exam-dialog-title">深度体检 · {{ nodeName }}</span>
        <div class="exam-dialog-status">
          <span class="status-light" :class="`status-light--${statusLight}`"></span>
          <el-tag :type="statusTag.type" size="small" effect="light">{{ statusTag.label }}</el-tag>
        </div>
      </div>
    </template>

    <!-- 一屏化双栏体检:所有检测项从打开即全占位,数据到达即填值,正在处理项高亮。
         与历史报告卡(ExamReportCard)复用同一 ExamReportLayout。 -->
    <ExamReportLayout
      :stability="metrics"
      :samples="samples"
      show-sparkline
      :stability-error="fatalError"
      :regions="regions"
      :region-active="regionActive"
      :unlocks="unlockResults"
      :unlock-active="unlockActive"
      :egress="egress"
      :egress-active="egressActive"
      :terminal="terminal"
    />

    <template #footer>
      <el-button v-if="running" :loading="cancelling" @click="cancel">取消体检</el-button>
      <el-button v-if="!running && terminal && shareable" @click="openShare">分享</el-button>
      <el-button v-if="!running && terminal" @click="rerun">重新体检</el-button>
      <el-button :disabled="running" type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <!-- 分享卡:完成态可把本次体检渲染成一张精心设计的分享 PNG。 -->
  <ExamShareDialog
    v-model:visible="shareVisible"
    :report="shareReport"
    :node-name="nodeName"
    :exam-time="shareTime"
  />
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import type {
  ExamEvent,
  ExamStabilityMetrics,
  ExamStabilitySample,
  ExamRegionResult,
  ExamUnlockResult,
  ExamEgressMetrics
} from '@/types'
import { upsertRegionRow } from './exam/regionspeed'
import { upsertUnlockRow } from './exam/unlock'
import { mergeEgress } from './exam/egress'
import { regionSectionComplete, egressSectionComplete } from './exam/examrows'
import { ExamStream } from './exam/examstream'
import type { ExamStreamStatus, EventSourceLike } from './exam/examstream'
import { computeStatusLight } from './exam/statuslight'
import ExamReportLayout from './exam/ExamReportLayout.vue'
import ExamShareDialog from './exam/ExamShareDialog.vue'
import type { ExamReport } from '@/types'

const visible = ref(false)
const nodeName = ref('')
const status = ref<ExamStreamStatus | 'idle'>('idle')
const cancelling = ref(false)

const samples = ref<ExamStabilitySample[]>([])
const metrics = ref<ExamStabilityMetrics | null>(null)
const regions = ref<ExamRegionResult[]>([])
const unlockResults = ref<ExamUnlockResult[]>([])
const egress = ref<ExamEgressMetrics | null>(null)
const terminalError = ref('')

let payload: { self_node_id?: number; node_key?: string } = {}
let stream: ExamStream | null = null

// 运行态:连接中 / 直播中 / 重连中(可取消)。
const running = computed(
  () => status.value === 'connecting' || status.value === 'live' || status.value === 'reconnecting'
)
// 终态:完成 / 已取消 / 连接失败(可重新体检)。
const terminal = computed(
  () => status.value === 'done' || status.value === 'cancelled' || status.value === 'error'
)

// 段进行中判定(驱动各段首个 waiting 行的高亮转移),新段序:出网 -> 稳定性 -> 多地域 -> 解锁。
// - 出网段前置:开检即进行中,3 类(ipv4/ipv6/dns)全到达即收尾;
// - 出网收尾后稳定性采样(metrics 到达即收尾);
// - 稳定性收尾后多地域段开测,8 固定区域全到达即收尾(基准可缺席不阻塞);
// - 多地域收尾后解锁段开测。
const egressComplete = computed(() => egressSectionComplete(egress.value))
const egressActive = computed(() => running.value && !egressComplete.value)
const stabilityDone = computed(() => metrics.value !== null)
const regionsComplete = computed(() => regionSectionComplete(regions.value))
const regionActive = computed(() => running.value && stabilityDone.value && !regionsComplete.value)
const unlockActive = computed(() => running.value && regionsComplete.value)

// 状态灯:绿(正常进行,无失败项)、黄(有失败项但非致命)、红(致命/连接失败)。
const statusLight = computed(() =>
  computeStatusLight(
    status.value,
    terminalError.value,
    metrics.value,
    regions.value,
    unlockResults.value,
    egress.value
  )
)

// 三态标签:完成/已取消/连接失败(终态)与连接中/体检中/重连中(运行态)互斥可区分。
const statusTag = computed<{
  label: string
  type: 'success' | 'info' | 'warning' | 'danger'
}>(() => {
  switch (status.value) {
    case 'connecting':
      return { label: '连接中', type: 'info' }
    case 'live':
      return { label: '体检中', type: 'info' }
    case 'reconnecting':
      return { label: '重连中…', type: 'warning' }
    case 'done':
      return { label: '完成', type: 'success' }
    case 'cancelled':
      return { label: '已取消', type: 'info' }
    case 'error':
      return { label: '连接失败', type: 'danger' }
    default:
      return { label: '准备中', type: 'info' }
  }
})

// 稳定性段错误框只在终态"连接失败/体检失败"时展示;重连中不视为错误。
const fatalError = computed(() =>
  status.value === 'error' ? terminalError.value || '连接失败' : ''
)

// 分享卡:完成态把当前各段状态汇聚成一份 ExamReport 交给分享对话框渲染。
const shareVisible = ref(false)
const shareTime = ref<number>(0)
const shareable = computed(
  () =>
    metrics.value !== null ||
    regions.value.length > 0 ||
    unlockResults.value.length > 0 ||
    egress.value !== null
)
const shareReport = computed<ExamReport>(() => ({
  stability: metrics.value ?? undefined,
  region_speed: { regions: regions.value },
  unlock: { results: unlockResults.value },
  egress: egress.value ?? undefined
}))
const openShare = () => {
  shareTime.value = Date.now()
  shareVisible.value = true
}

const open = (p: { self_node_id?: number; node_key?: string }, name: string) => {
  payload = p
  nodeName.value = name
  // Reset state to prepare for new/reattached stream
  reset()
  visible.value = true
  // runExam without force: attaches to existing task if running, or starts new if none
  runExam()
}

const reset = () => {
  samples.value = []
  metrics.value = null
  regions.value = []
  unlockResults.value = []
  egress.value = null
  terminalError.value = ''
  cancelling.value = false
  status.value = 'idle'
}

const buildParams = () => {
  const params = new URLSearchParams()
  if (payload.self_node_id) params.set('self_node_id', String(payload.self_node_id))
  if (payload.node_key) params.set('node_key', payload.node_key)
  return params
}

const runExam = (force = false) => {
  stream?.dispose()
  stream = new ExamStream(
    {
      createEventSource: (url: string): EventSourceLike =>
        new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
      fetch: window.fetch.bind(window)
    },
    { onFrame, onStatus, onTerminal }
  )
  const params = buildParams()
  if (force) params.set('force', '1') // 重新体检:已收口的旧任务强制重开,不回放上次结果
  stream.start(`/api/nodes/exam/stream?${params}`)
}

const onFrame = (frame: ExamEvent) => {
  if (frame.phase === 'sample' && frame.sample) {
    samples.value = [...samples.value, frame.sample]
  } else if (frame.phase === 'region' && frame.region) {
    regions.value = upsertRegionRow(regions.value, frame.region)
  } else if (frame.phase === 'unlock' && frame.unlock_result) {
    unlockResults.value = upsertUnlockRow(unlockResults.value, frame.unlock_result)
  } else if (frame.phase === 'egress' && frame.egress) {
    // 逐条到达:按子项不可变叠加(IPv4/IPv6/DNS 各带一类)。
    egress.value = mergeEgress(egress.value, frame.egress)
  } else if (frame.phase === 'section_done' && frame.section === 'egress' && frame.egress) {
    // 段末权威覆盖(含 DNS 泄露判定)。
    egress.value = frame.egress
  } else if (frame.phase === 'section_done' && frame.metrics) {
    metrics.value = frame.metrics
  }
}

const onStatus = (s: ExamStreamStatus) => {
  status.value = s
  if (s === 'cancelled' || s === 'done' || s === 'error') cancelling.value = false
}

// 终态帧携带的信息:error 帧带失败文案。
const onTerminal = (frame: ExamEvent) => {
  if (frame.phase === 'error') terminalError.value = frame.error || '体检失败'
}

const cancel = () => {
  if (!stream || cancelling.value) return
  cancelling.value = true
  stream.cancel(`/api/nodes/exam/cancel?${buildParams()}`).catch((err) => {
    console.error('cancel exam error', err)
    cancelling.value = false
  })
}

const rerun = () => {
  reset()
  runExam(true)
}

const onClose = () => {
  // When closing during exam, show toast about background continuation
  if (running.value) {
    ElMessage({
      message: '体检任务将在后台继续运行,可随时回来查看进度',
      type: 'info',
      duration: 3000
    })
  }
}

const onClosed = () => {
  // Dispose stream (closes SSE connection) but does NOT cancel the task
  stream?.dispose()
  stream = null
  if (!running.value) reset()
}

defineExpose({
  open,
  onClose,
  onClosed,
  visible,
  running,
  terminal,
  samples,
  regions,
  unlockResults,
  rerun
})
</script>

<style scoped>
.exam-dialog-head {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
}
.exam-dialog-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.exam-dialog-status {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.status-light {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
}
.status-light--green {
  background-color: var(--ph-success);
}
.status-light--yellow {
  background-color: var(--ph-warning);
}
.status-light--red {
  background-color: var(--ph-danger);
}
</style>
