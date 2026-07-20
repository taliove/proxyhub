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
    <!-- 稳定性区(第一段)。后续段落(多地域测速、解锁)在此卡片下方按序追加。 -->
    <section class="exam-section">
      <header class="exam-section-head">
        <span class="exam-section-title">稳定性</span>
        <span class="exam-section-sub">{{ phaseText }}</span>
      </header>

      <!-- 评分环 + 关键指标 -->
      <div class="exam-stability">
        <div class="exam-score" :style="{ color: scoreColor }">
          <div class="exam-score-value">{{ metrics ? metrics.score : sampledScore }}</div>
          <div class="exam-score-label">{{ scoreText }}</div>
        </div>

        <div class="exam-metrics">
          <div class="exam-metric">
            <span class="exam-metric-label">丢包率</span>
            <span class="exam-metric-value">{{ lossText }}</span>
          </div>
          <div class="exam-metric">
            <span class="exam-metric-label">中位</span>
            <span class="exam-metric-value">{{ medianText }}</span>
          </div>
          <div class="exam-metric">
            <span class="exam-metric-label">P95</span>
            <span class="exam-metric-value">{{ p95Text }}</span>
          </div>
          <div class="exam-metric">
            <span class="exam-metric-label">P99</span>
            <span class="exam-metric-value">{{ p99Text }}</span>
          </div>
          <div class="exam-metric">
            <span class="exam-metric-label">抖动</span>
            <span class="exam-metric-value">{{ jitterText }}</span>
          </div>
        </div>
      </div>

      <!-- 延迟序列 sparkline(只画成功样本;丢包点不连线) -->
      <svg class="exam-sparkline" :viewBox="`0 0 ${SPARK_W} ${SPARK_H}`" preserveAspectRatio="none">
        <polyline
          v-if="sparkPath"
          :points="sparkPath"
          fill="none"
          :stroke="sparkColor"
          stroke-width="2"
          stroke-linejoin="round"
          stroke-linecap="round"
        />
        <text v-else x="50%" y="50%" class="exam-spark-empty" text-anchor="middle">等待采样…</text>
      </svg>

      <el-alert
        v-if="errorText"
        type="error"
        :closable="false"
        :title="errorText"
        class="exam-error"
      />
    </section>

    <!-- 多地域测速区(第二段):8 区串行,随 SSE 逐行到达。 -->
    <section class="exam-section">
      <header class="exam-section-head">
        <span class="exam-section-title">多地域测速</span>
        <span class="exam-section-sub">{{ regionPhaseText }}</span>
      </header>

      <table v-if="regions.length" class="exam-region-table">
        <thead>
          <tr>
            <th>区域</th>
            <th>延迟</th>
            <th>下行速率</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in regions" :key="r.code">
            <td>{{ r.name }}</td>
            <td>{{ regionRowStatus(r) === 'error' ? '-' : formatTtfb(r.ttfb_ms) }}</td>
            <td>{{ regionRowStatus(r) === 'error' ? '-' : formatMbps(r.down_mbps) }}</td>
            <td :class="`exam-region-status exam-region-status-${regionRowStatus(r)}`">
              {{ regionStatusText(r) }}
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="exam-region-empty">等待多地域测速…</div>
    </section>

    <!-- 解锁区(第三段):6 个目标并行判定,随 SSE 逐个到达。 -->
    <UnlockSection :results="unlockResults" :phase-text="unlockPhaseText" />

    <template #footer>
      <el-button v-if="!running && done" @click="rerun">重新体检</el-button>
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
import {
  scoreColorVar,
  scoreLabel,
  formatMs,
  formatLossPct,
  buildSparklinePoints,
  buildSparklinePath
} from './exam/stability'
import {
  regionRowStatus,
  regionStatusText,
  formatTtfb,
  formatMbps,
  upsertRegionRow
} from './exam/regionspeed'
import { upsertUnlockRow } from './exam/unlock'
import UnlockSection from './exam/UnlockSection.vue'

const SPARK_W = 300
const SPARK_H = 56

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

// 采样过程中的临时评分(段完成前给个即时反馈):成功率粗估,段完成后被精确 metrics 覆盖。
const sampledScore = computed(() => {
  if (samples.value.length === 0) return 0
  const ok = samples.value.filter((s) => s.ok).length
  return Math.round((ok / samples.value.length) * 100)
})

const effectiveScore = computed(() => (metrics.value ? metrics.value.score : sampledScore.value))

const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

const scoreColor = computed(() => cssVar(scoreColorVar(effectiveScore.value)) || '#059669')
const sparkColor = computed(() => cssVar('--ph-color-primary') || '#4f46e5')
const scoreText = computed(() => scoreLabel(effectiveScore.value))

const lossText = computed(() => formatLossPct(metrics.value?.loss_rate))
const medianText = computed(() => formatMs(metrics.value?.median_ms))
const p95Text = computed(() => formatMs(metrics.value?.p95_ms))
const p99Text = computed(() => formatMs(metrics.value?.p99_ms))
const jitterText = computed(() => formatMs(metrics.value?.jitter_ms))

const sparkPath = computed(() =>
  buildSparklinePath(buildSparklinePoints(samples.value, SPARK_W, SPARK_H))
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

<style scoped>
.exam-section {
  padding: var(--ph-space-2) 0;
}
.exam-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: var(--ph-space-3);
}
.exam-section-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
}
.exam-section-sub {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.exam-stability {
  display: flex;
  align-items: center;
  gap: var(--ph-space-5);
}
.exam-score {
  text-align: center;
  min-width: 96px;
}
.exam-score-value {
  font-size: var(--ph-text-display);
  font-weight: 700;
  line-height: 1;
}
.exam-score-label {
  font-size: var(--ph-text-sm);
  margin-top: var(--ph-space-1);
}
.exam-metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(72px, 1fr));
  gap: var(--ph-space-3);
  flex: 1;
}
.exam-metric {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-1);
}
.exam-metric-label {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-metric-value {
  font-size: var(--ph-text-md);
  font-weight: 600;
}
.exam-sparkline {
  width: 100%;
  height: 56px;
  margin-top: var(--ph-space-4);
  display: block;
}
.exam-spark-empty {
  font-size: 12px;
  fill: var(--ph-text-secondary);
}
.exam-error {
  margin-top: var(--ph-space-3);
}
.exam-region-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--ph-text-sm);
}
.exam-region-table th,
.exam-region-table td {
  padding: var(--ph-space-2) var(--ph-space-3);
  text-align: left;
  border-bottom: 1px solid var(--ph-border-light);
}
.exam-region-table th {
  font-weight: 600;
  color: var(--ph-text-secondary);
}
.exam-region-status {
  font-weight: 600;
}
.exam-region-status-ok {
  color: var(--ph-success);
}
.exam-region-status-error {
  color: var(--ph-danger);
}
.exam-region-empty {
  padding: var(--ph-space-4);
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
</style>
