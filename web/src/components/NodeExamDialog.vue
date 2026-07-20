<template>
  <el-dialog
    v-model="visible"
    width="960px"
    class="exam-dialog"
    :close-on-click-modal="!running"
    :close-on-press-escape="!running"
    :show-close="!running"
    @closed="onClosed"
  >
    <!-- 头部:标题 + 三态标签(连接中/体检中/重连中/完成/已取消/连接失败)。 -->
    <template #header>
      <div class="exam-dialog-head">
        <span class="exam-dialog-title">深度体检 · {{ nodeName }}</span>
        <el-tag :type="statusTag.type" size="small" effect="light">{{ statusTag.label }}</el-tag>
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
      <el-button v-if="!running && terminal" @click="rerun">重新体检</el-button>
      <el-button :disabled="running" type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
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
import { regionSectionComplete } from './exam/examrows'
import { ExamStream } from './exam/examstream'
import type { ExamStreamStatus, EventSourceLike } from './exam/examstream'
import ExamReportLayout from './exam/ExamReportLayout.vue'

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

// 段进行中判定(驱动各段首个 waiting 行的高亮转移):
// - 稳定性收尾(metrics 到达)后多地域段开测,8 固定区域全到达即收尾(基准可缺席不阻塞);
// - 多地域收尾后解锁与出网并行开测。
const stabilityDone = computed(() => metrics.value !== null)
const regionsComplete = computed(() => regionSectionComplete(regions.value))
const regionActive = computed(() => running.value && stabilityDone.value && !regionsComplete.value)
const unlockActive = computed(() => running.value && regionsComplete.value)
const egressActive = computed(() => running.value && regionsComplete.value)

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

const open = (p: { self_node_id?: number; node_key?: string }, name: string) => {
  payload = p
  nodeName.value = name
  reset()
  visible.value = true
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

const onClosed = () => {
  stream?.dispose()
  stream = null
  if (!running.value) reset()
}

defineExpose({ open })
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
</style>
