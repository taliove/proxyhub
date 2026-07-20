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
    <!-- 三段串行体检,随 SSE 分段推送。段组件与历史报告卡(ExamReportCard)复用同一实现。 -->
    <StabilitySection
      :metrics="metrics"
      :samples="samples"
      :sub-text="phaseText"
      :error="errorText"
    />
    <RegionSpeedSection :regions="regions" :sub-text="regionPhaseText" />
    <UnlockSection :results="unlockResults" :phase-text="unlockPhaseText" />

    <template #footer>
      <el-button v-if="!running && done" @click="rerun">重新体检</el-button>
      <el-button :disabled="running" type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type {
  ExamEvent,
  ExamStabilityMetrics,
  ExamStabilitySample,
  ExamRegionResult,
  ExamUnlockResult
} from '@/types'
import { upsertRegionRow } from './exam/regionspeed'
import { upsertUnlockRow } from './exam/unlock'
import StabilitySection from './exam/StabilitySection.vue'
import RegionSpeedSection from './exam/RegionSpeedSection.vue'
import UnlockSection from './exam/UnlockSection.vue'

const visible = ref(false)
const running = ref(false)
const done = ref(false)
const nodeName = ref('')
const phaseText = ref('准备中…')
const errorText = ref('')
const regionPhaseText = ref('等待中…')
const unlockPhaseText = ref('等待中…')

const samples = ref<ExamStabilitySample[]>([])
const metrics = ref<ExamStabilityMetrics | null>(null)
const regions = ref<ExamRegionResult[]>([])
const unlockResults = ref<ExamUnlockResult[]>([])

let payload: { self_node_id?: number; node_key?: string } = {}
let es: EventSource | null = null
let finished = false

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
  errorText.value = ''
  regionPhaseText.value = '等待中…'
  unlockPhaseText.value = '等待中…'
  done.value = false
}

const runExam = () => {
  running.value = true
  finished = false
  phaseText.value = '稳定性采样中…'

  const params = new URLSearchParams()
  if (payload.self_node_id) params.set('self_node_id', String(payload.self_node_id))
  if (payload.node_key) params.set('node_key', payload.node_key)

  es = new EventSource(`/api/nodes/exam/stream?${params}`, { withCredentials: true })
  es.onmessage = onFrame
  es.onerror = onError
}

const onFrame = (e: MessageEvent) => {
  let frame: ExamEvent
  try {
    frame = JSON.parse(e.data)
  } catch (err) {
    console.error('parse exam SSE frame error', err)
    return
  }

  if (frame.phase === 'sample' && frame.sample) {
    samples.value = [...samples.value, frame.sample]
  } else if (frame.phase === 'region' && frame.region) {
    regions.value = upsertRegionRow(regions.value, frame.region)
    regionPhaseText.value = '多地域测速中…'
  } else if (frame.phase === 'unlock' && frame.unlock_result) {
    unlockResults.value = upsertUnlockRow(unlockResults.value, frame.unlock_result)
    unlockPhaseText.value = '解锁检测中…'
  } else if (frame.phase === 'section_done' && frame.section === 'region_speed') {
    regionPhaseText.value = '多地域测速完成'
  } else if (frame.phase === 'section_done' && frame.section === 'unlock') {
    unlockPhaseText.value = '解锁检测完成'
  } else if (frame.phase === 'section_done' && frame.metrics) {
    metrics.value = frame.metrics
    phaseText.value = '稳定性完成'
  } else if (frame.phase === 'done') {
    finish()
  } else if (frame.phase === 'error') {
    errorText.value = frame.error || '体检失败'
    finish()
  }
}

const onError = () => {
  if (finished) return
  errorText.value = '连接断开'
  finish()
}

const finish = () => {
  finished = true
  done.value = true
  running.value = false
  es?.close()
  es = null
}

const rerun = () => {
  reset()
  runExam()
}

const onClosed = () => {
  es?.close()
  es = null
  if (!running.value) reset()
}

defineExpose({ open })
</script>
