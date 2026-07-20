<template>
  <el-dialog
    v-model="visible"
    :title="`深度体检 · ${nodeName}`"
    width="560px"
    :close-on-click-modal="!running"
    :close-on-press-escape="!running"
    :show-close="!running"
    @closed="onClosed"
  >
    <!-- 顶部总进度:三段加权进度条 + 当前段计数 + 终态/运行态标签(三态可区分)。 -->
    <div class="exam-progress">
      <div class="exam-progress-head">
        <span class="exam-progress-counter">{{ counterText }}</span>
        <el-tag :type="statusTag.type" size="small" effect="light">{{ statusTag.label }}</el-tag>
      </div>
      <div class="exam-progress-track">
        <div
          class="exam-progress-fill"
          :class="`exam-progress-fill-${statusTag.tone}`"
          :style="{ width: `${progressPercent}%` }"
        />
      </div>
    </div>

    <!-- 三段串行体检,随 SSE 分段推送。段组件与历史报告卡(ExamReportCard)复用同一实现。 -->
    <StabilitySection
      :metrics="metrics"
      :samples="samples"
      :sub-text="phaseText"
      :error="fatalError"
    />
    <RegionSpeedSection :regions="regions" :sub-text="regionPhaseText" />
    <UnlockSection :results="unlockResults" :phase-text="unlockPhaseText" />

    <!-- 底部滚动日志:最近 ~8 条人类可读行,从各类帧生成。 -->
    <div v-if="logLines.length" class="exam-log">
      <div v-for="(line, i) in logLines" :key="i" class="exam-log-line">{{ line }}</div>
    </div>

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
  ExamUnlockResult
} from '@/types'
import { upsertRegionRow } from './exam/regionspeed'
import { upsertUnlockRow } from './exam/unlock'
import { examProgressPercent, examSegmentCounter } from './exam/progress'
import { examLogLine, appendExamLog } from './exam/examlog'
import { ExamStream } from './exam/examstream'
import type { ExamStreamStatus, EventSourceLike } from './exam/examstream'
import StabilitySection from './exam/StabilitySection.vue'
import RegionSpeedSection from './exam/RegionSpeedSection.vue'
import UnlockSection from './exam/UnlockSection.vue'

const visible = ref(false)
const nodeName = ref('')
const status = ref<ExamStreamStatus | 'idle'>('idle')
const cancelling = ref(false)

const samples = ref<ExamStabilitySample[]>([])
const metrics = ref<ExamStabilityMetrics | null>(null)
const regions = ref<ExamRegionResult[]>([])
const unlockResults = ref<ExamUnlockResult[]>([])
const logLines = ref<string[]>([])
const currentRegionName = ref('')
const currentUnlockName = ref('')
const terminalError = ref('')

let payload: { self_node_id?: number; node_key?: string } = {}
let stream: ExamStream | null = null

// 运行态:连接中 / 直播中 / 重连中(可取消,进度条推进)。
const running = computed(
  () => status.value === 'connecting' || status.value === 'live' || status.value === 'reconnecting'
)
// 终态:完成 / 已取消 / 连接失败(可重新体检)。
const terminal = computed(
  () => status.value === 'done' || status.value === 'cancelled' || status.value === 'error'
)

// 稳定性段是否收尾(metrics 到达)用于推进分段计数。
const counts = computed(() => ({
  samples: samples.value.length,
  regions: regions.value.length,
  unlocks: unlockResults.value.length,
  stabilityDone: metrics.value !== null
}))

const progressPercent = computed(() =>
  status.value === 'done' ? 100 : examProgressPercent(counts.value)
)

const counterText = computed(
  () =>
    examSegmentCounter(counts.value, {
      regionName: currentRegionName.value,
      unlockName: currentUnlockName.value
    }).text
)

// 状态标签:三态(完成/已取消/连接失败)与运行态互斥可区分;tone 同时驱动进度条配色。
const statusTag = computed<{
  label: string
  type: 'success' | 'info' | 'warning' | 'danger'
  tone: string
}>(() => {
  switch (status.value) {
    case 'connecting':
      return { label: '连接中', type: 'info', tone: 'live' }
    case 'live':
      return { label: '体检中', type: 'info', tone: 'live' }
    case 'reconnecting':
      return { label: '重连中…', type: 'warning', tone: 'warn' }
    case 'done':
      return { label: '完成', type: 'success', tone: 'done' }
    case 'cancelled':
      return { label: '已取消', type: 'info', tone: 'cancelled' }
    case 'error':
      return { label: '连接失败', type: 'danger', tone: 'error' }
    default:
      return { label: '准备中', type: 'info', tone: 'live' }
  }
})

// 稳定性段错误框只在终态"连接失败/体检失败"时展示;重连中不视为错误。
const fatalError = computed(() =>
  status.value === 'error' ? terminalError.value || '连接失败' : ''
)

const phaseText = computed(() => {
  if (status.value === 'reconnecting') return '连接中断,重连中…'
  if (metrics.value) return '稳定性完成'
  if (running.value) return '稳定性采样中…'
  return '准备中…'
})
const regionPhaseText = computed(() => {
  if (regions.value.length === 0) return '等待中…'
  return regions.value.length >= 8 ? '多地域测速完成' : '多地域测速中…'
})
const unlockPhaseText = computed(() => {
  if (unlockResults.value.length === 0) return '等待中…'
  return unlockResults.value.length >= 6 ? '解锁检测完成' : '解锁检测中…'
})

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
  logLines.value = []
  currentRegionName.value = ''
  currentUnlockName.value = ''
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

const pushLog = (frame: ExamEvent) => {
  logLines.value = appendExamLog(logLines.value, examLogLine(frame))
}

const onFrame = (frame: ExamEvent) => {
  if (frame.phase === 'sample' && frame.sample) {
    samples.value = [...samples.value, frame.sample]
  } else if (frame.phase === 'region' && frame.region) {
    regions.value = upsertRegionRow(regions.value, frame.region)
    currentRegionName.value = frame.region.name
  } else if (frame.phase === 'unlock' && frame.unlock_result) {
    unlockResults.value = upsertUnlockRow(unlockResults.value, frame.unlock_result)
    currentUnlockName.value = frame.unlock_result.target_name
  } else if (frame.phase === 'section_done' && frame.metrics) {
    metrics.value = frame.metrics
  }
  pushLog(frame)
}

const onStatus = (s: ExamStreamStatus) => {
  status.value = s
  if (s === 'cancelled' || s === 'done' || s === 'error') cancelling.value = false
}

// 终态帧携带的信息:error 帧带失败文案,cancelled/done 帧补一条日志。
const onTerminal = (frame: ExamEvent) => {
  if (frame.phase === 'error') terminalError.value = frame.error || '体检失败'
  pushLog(frame)
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
.exam-progress {
  margin-bottom: var(--ph-space-4);
}
.exam-progress-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--ph-space-2);
}
.exam-progress-counter {
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-regular);
}
.exam-progress-track {
  height: 6px;
  border-radius: var(--ph-radius-full);
  background: var(--ph-bg-hover);
  overflow: hidden;
}
.exam-progress-fill {
  height: 100%;
  border-radius: var(--ph-radius-full);
  transition: width 0.3s ease;
  background: var(--ph-color-primary);
}
.exam-progress-fill-done {
  background: var(--ph-success);
}
.exam-progress-fill-warn {
  background: var(--ph-warning);
}
.exam-progress-fill-error {
  background: var(--ph-danger);
}
.exam-progress-fill-cancelled {
  background: var(--ph-text-secondary);
}
.exam-log {
  margin-top: var(--ph-space-4);
  padding: var(--ph-space-3);
  border-radius: var(--ph-radius-sm);
  background: var(--ph-bg-hover);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.7;
  max-height: 132px;
  overflow-y: auto;
}
.exam-log-line {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
