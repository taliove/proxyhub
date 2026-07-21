<template>
  <div v-if="hasData" class="trend-section">
    <h4>📉 历史趋势</h4>
    <svg class="trend-chart" viewBox="0 0 400 80" xmlns="http://www.w3.org/2000/svg">
      <polyline
        :points="trendPoints"
        fill="none"
        stroke="#409eff"
        stroke-width="2"
        stroke-linejoin="round"
      />
      <circle
        v-for="(point, idx) in trendPointsArray"
        :key="idx"
        :cx="point.x"
        :cy="point.y"
        r="3"
        :fill="getPointColor(point.score)"
      />
    </svg>
    <div class="trend-legend">
      <span class="muted">最近 {{ scoredRuns.length }} 次测试</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { TestRun } from '@/composables/useAirportTest'

interface Props {
  runs: TestRun[]
}

const props = defineProps<Props>()

const scoredRuns = computed(() => {
  return props.runs.filter((r) => r.overall_score != null)
})

const hasData = computed(() => scoredRuns.value.length > 0)

const trendPointsArray = computed(() => {
  const runs = [...scoredRuns.value].reverse()
  if (runs.length === 0) return []

  const width = 400
  const height = 80
  const padding = 10

  return runs.map((run, idx) => {
    const x = padding + (idx / Math.max(runs.length - 1, 1)) * (width - 2 * padding)
    const y = height - padding - (run.overall_score! / 100) * (height - 2 * padding)
    return { x, y, score: run.overall_score! }
  })
})

const trendPoints = computed(() => {
  return trendPointsArray.value.map((p) => `${p.x},${p.y}`).join(' ')
})

function getPointColor(score: number): string {
  if (score >= 80) return '#67c23a'
  if (score >= 60) return '#e6a23c'
  return '#f56c6c'
}
</script>

<style scoped>
.trend-section {
  margin-top: 24px;
}

.trend-section h4 {
  margin-bottom: 12px;
}

.trend-chart {
  width: 100%;
  height: 80px;
  background: var(--el-fill-color-lighter);
  border-radius: 4px;
}

.trend-legend {
  margin-top: 8px;
  text-align: center;
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
