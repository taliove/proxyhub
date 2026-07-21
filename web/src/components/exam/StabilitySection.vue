<template>
  <!-- 稳定性段(体检第一段)。同时服务实时体检(samples 流式到达、metrics 段末覆盖)
       与历史报告卡(仅有 metrics、无原始采样序列 -> show-sparkline=false)。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">稳定性</span>
      <span v-if="subText" class="exam-section-sub">{{ subText }}</span>
    </header>

    <div class="exam-stability">
      <div class="exam-score" :style="{ color: scoreColor }">
        <div class="exam-score-value">{{ effectiveScore }}</div>
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

    <!-- 延迟序列 sparkline(只画成功样本;丢包点不连线)。历史报告无采样序列,不渲染。 -->
    <div v-if="showSparkline" class="exam-sparkline-container">
      <svg
        class="exam-sparkline"
        :viewBox="`0 0 ${SPARK_W} ${SPARK_H}`"
        preserveAspectRatio="xMidYMid meet"
      >
        <!-- Y-axis ticks (latency scale) -->
        <g v-if="yAxisTicks.length > 0" class="y-axis">
          <text
            v-for="tick in yAxisTicks"
            :key="tick.value"
            :x="0"
            :y="tick.y"
            class="axis-label axis-label-y"
            text-anchor="start"
            dominant-baseline="middle"
          >
            {{ tick.label }}
          </text>
        </g>

        <!-- X-axis ticks (time scale) -->
        <g v-if="xAxisTicks.length > 0" class="x-axis">
          <text
            v-for="tick in xAxisTicks"
            :key="tick.value"
            :x="tick.x"
            :y="SPARK_H"
            class="axis-label axis-label-x"
            text-anchor="middle"
            dominant-baseline="hanging"
          >
            {{ tick.label }}
          </text>
        </g>

        <!-- Sparkline polyline -->
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
    </div>

    <el-alert v-if="error" type="error" :closable="false" :title="error" class="exam-error" />
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamStabilityMetrics, ExamStabilitySample } from '@/types'
import {
  scoreColorVar,
  scoreLabel,
  formatMs,
  formatLossPct,
  buildSparklinePoints,
  buildSparklinePath
} from './stability'
import {
  computeSparklineYAxis,
  computeSparklineXAxis,
  computeSparklineLayout
} from './sparklineaxes'

const SPARK_W = 300
const SPARK_H = 56

const props = withDefaults(
  defineProps<{
    metrics: ExamStabilityMetrics | null
    samples?: ExamStabilitySample[]
    subText?: string
    error?: string
    showSparkline?: boolean
  }>(),
  { samples: () => [], subText: '', error: '', showSparkline: true }
)

// 采样过程中的临时评分(段完成前给即时反馈):成功率粗估,段完成后被精确 metrics 覆盖。
const sampledScore = computed(() => {
  if (props.samples.length === 0) return 0
  const ok = props.samples.filter((s) => s.ok).length
  return Math.round((ok / props.samples.length) * 100)
})

const effectiveScore = computed(() => (props.metrics ? props.metrics.score : sampledScore.value))

const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

const scoreColor = computed(() => cssVar(scoreColorVar(effectiveScore.value)) || '#059669')
const sparkColor = computed(() => cssVar('--ph-color-primary') || '#4f46e5')
const scoreText = computed(() => scoreLabel(effectiveScore.value))

const lossText = computed(() => formatLossPct(props.metrics?.loss_rate))
const medianText = computed(() => formatMs(props.metrics?.median_ms))
const p95Text = computed(() => formatMs(props.metrics?.p95_ms))
const p99Text = computed(() => formatMs(props.metrics?.p99_ms))
const jitterText = computed(() => formatMs(props.metrics?.jitter_ms))

const layout = computed(() => computeSparklineLayout(props.samples, SPARK_W, SPARK_H))

const sparkPath = computed(() =>
  buildSparklinePath(
    buildSparklinePoints(
      props.samples,
      SPARK_W,
      SPARK_H,
      layout.value.plotAreaOffsetX,
      layout.value.plotAreaOffsetY
    )
  )
)

const yAxisTicks = computed(() =>
  computeSparklineYAxis(props.samples, SPARK_H, layout.value.plotAreaOffsetY)
)
const xAxisTicks = computed(() =>
  computeSparklineXAxis(props.samples, SPARK_W, layout.value.plotAreaOffsetX)
)
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
.exam-sparkline-container {
  margin-top: var(--ph-space-4);
  width: 100%;
  aspect-ratio: 300 / 70;
  max-height: 80px;
}
.exam-sparkline {
  width: 100%;
  height: 100%;
  display: block;
}
.axis-label {
  font-size: 9px;
  fill: var(--ph-text-tertiary);
  font-family:
    system-ui,
    -apple-system,
    sans-serif;
}
.axis-label-y {
  transform: translateX(2px);
}
.axis-label-x {
  transform: translateY(2px);
}
.exam-spark-empty {
  font-size: 12px;
  fill: var(--ph-text-secondary);
}
.exam-error {
  margin-top: var(--ph-space-3);
}
</style>
