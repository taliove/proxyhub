<template>
  <el-dialog
    v-model="visible"
    width="720px"
    class="stability-dialog"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    :show-close="true"
    @close="onClose"
    @closed="onClosed"
  >
    <template #header>
      <div class="stability-dialog-head">
        <span class="stability-dialog-title">出网+稳定性 · {{ nodeName }}</span>
        <el-tag :type="statusTag.type" size="small" effect="light">{{ statusTag.label }}</el-tag>
      </div>
    </template>

    <!-- 出网+稳定性只有两段(出网画像 + 稳定性评分),复用体检各段组件单栏呈现;
         不含多地域/解锁(动作语义:只查通不通、稳不稳)。 -->
    <div class="stability-body">
      <EgressSection :egress="egress" :active="egressActive" :terminal="terminal" />
      <StabilitySection :metrics="metrics" :samples="samples" show-sparkline :error="fatalError" />
    </div>

    <template #footer>
      <el-button v-if="running" :loading="cancelling" @click="cancel">取消检查</el-button>
      <el-button v-if="!running && terminal" @click="rerun">重新检查</el-button>
      <el-button :disabled="running" type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import type {
  ExamEvent,
  ExamStabilityMetrics,
  ExamStabilitySample,
  ExamEgressMetrics
} from '@/types'
import { mergeEgress } from './exam/egress'
import { ExamStream } from './exam/examstream'
import type { ExamStreamStatus, EventSourceLike } from './exam/examstream'
import EgressSection from './exam/EgressSection.vue'
import StabilitySection from './exam/StabilitySection.vue'

// 单节点「出网+稳定性」检查对话框(检查动作 2):复用体检 SSE 机制(ExamStream)与出网/稳定性
// 两段组件,消费 /api/nodes/stability/stream。与 NodeExamDialog 同构但只有两段——不含多地域/解锁。
// 任务化:进行中重开自动附加(回放+续传),后台不因关窗中断。

const visible = ref(false)
const nodeName = ref('')
const status = ref<ExamStreamStatus | 'idle'>('idle')
const cancelling = ref(false)

const samples = ref<ExamStabilitySample[]>([])
const metrics = ref<ExamStabilityMetrics | null>(null)
const egress = ref<ExamEgressMetrics | null>(null)
const terminalError = ref('')

let payload: { self_node_id?: number; node_key?: string } = {}
let stream: ExamStream | null = null

const running = computed(
  () => status.value === 'connecting' || status.value === 'live' || status.value === 'reconnecting'
)
const terminal = computed(
  () => status.value === 'done' || status.value === 'cancelled' || status.value === 'error'
)

// 出网段进行中判定:开检即进行中,稳定性 metrics 到达前一直高亮出网首个 waiting 行。
const egressActive = computed(() => running.value && metrics.value === null)

const statusTag = computed<{ label: string; type: 'success' | 'info' | 'warning' | 'danger' }>(
  () => {
    switch (status.value) {
      case 'connecting':
        return { label: '连接中', type: 'info' }
      case 'live':
        return { label: '检查中', type: 'info' }
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
  }
)

const fatalError = computed(() =>
  status.value === 'error' ? terminalError.value || '连接失败' : ''
)

const open = (p: { self_node_id?: number; node_key?: string }, name: string) => {
  payload = p
  nodeName.value = name
  reset()
  visible.value = true
  runCheck()
}

const reset = () => {
  samples.value = []
  metrics.value = null
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

const runCheck = (force = false) => {
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
  if (force) params.set('force', '1')
  stream.start(`/api/nodes/stability/stream?${params}`)
}

const onFrame = (frame: ExamEvent) => {
  if (frame.phase === 'sample' && frame.sample) {
    samples.value = [...samples.value, frame.sample]
  } else if (frame.phase === 'egress' && frame.egress) {
    egress.value = mergeEgress(egress.value, frame.egress)
  } else if (frame.phase === 'section_done' && frame.section === 'egress' && frame.egress) {
    egress.value = frame.egress
  } else if (frame.phase === 'section_done' && frame.metrics) {
    metrics.value = frame.metrics
  }
}

const onStatus = (s: ExamStreamStatus) => {
  status.value = s
  if (s === 'cancelled' || s === 'done' || s === 'error') cancelling.value = false
}

const onTerminal = (frame: ExamEvent) => {
  if (frame.phase === 'error') terminalError.value = frame.error || '检查失败'
}

const cancel = () => {
  if (!stream || cancelling.value) return
  cancelling.value = true
  stream.cancel(`/api/nodes/stability/cancel?${buildParams()}`).catch((err) => {
    console.error('cancel stability error', err)
    cancelling.value = false
  })
}

const rerun = () => {
  reset()
  runCheck(true)
}

const onClose = () => {
  if (running.value) {
    ElMessage({
      message: '检查任务将在后台继续运行,可随时回来查看进度',
      type: 'info',
      duration: 3000
    })
  }
}

const onClosed = () => {
  stream?.dispose()
  stream = null
  if (!running.value) reset()
}

defineExpose({ open, visible, running, terminal })
</script>

<style scoped>
.stability-dialog-head {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
}
.stability-dialog-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.stability-body {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-4);
}
</style>
