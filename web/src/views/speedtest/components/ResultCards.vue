<template>
  <div class="result-area">
    <div v-if="running" class="phase-line">
      <span class="phase-tag">第 {{ phaseIndex }}/3 阶段</span>
      <span>{{ phaseText }}</span>
    </div>
    <div class="cards">
      <div
        v-for="card in cards"
        :key="card.label"
        class="stat-card"
        :class="{ active: card.active }"
      >
        <div class="stat-value num">{{ card.value }}</div>
        <div class="stat-label">{{ card.label }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SpeedtestOutcome, SpeedtestPhase } from '../runner'

// 大数字结果区:测速中当前阶段的下行/上行卡实时刷新 liveMbps(fast.com 式跳动);
// 延迟/抖动在 latency 帧到达后即有值(经 idleLatencyMs/jitterMs prop)。完成后定格最终值。
const props = defineProps<{
  running: boolean
  phase: SpeedtestPhase | null
  liveMbps: number
  idleLatencyMs: number
  jitterMs: number
  result: SpeedtestOutcome | null
}>()

const PHASE_ORDER: SpeedtestPhase[] = ['latency', 'download', 'upload']
const PHASE_TEXT: Record<SpeedtestPhase, string> = {
  latency: '空闲延迟/抖动探测中',
  download: '下行测速中',
  upload: '上行测速中'
}

const phaseIndex = computed(() => (props.phase ? PHASE_ORDER.indexOf(props.phase) + 1 : 1))
const phaseText = computed(() => (props.phase ? PHASE_TEXT[props.phase] : ''))

const fmt = (v: number | undefined, digits: number): string =>
  v === undefined ? '—' : v.toFixed(digits)

// 测速中:下行/上行卡显示 liveMbps 实时跳动;延迟/抖动显示已测值(0 显示占位);
// 已完成:显示最终值。
const cards = computed(() => {
  const r = props.result
  const lat = r ? r.idleLatencyMs : props.idleLatencyMs
  const jit = r ? r.jitterMs : props.jitterMs
  return [
    {
      label: '下行 Mbps',
      value:
        props.running && props.phase === 'download' ? fmt(props.liveMbps, 1) : fmt(r?.downMbps, 1),
      active: props.running && props.phase === 'download'
    },
    {
      label: '上行 Mbps',
      value: props.running && props.phase === 'upload' ? fmt(props.liveMbps, 1) : fmt(r?.upMbps, 1),
      active: props.running && props.phase === 'upload'
    },
    {
      label: '空闲延迟 ms',
      value: lat > 0 ? fmt(lat, 0) : props.running ? '…' : '—',
      active: props.running && props.phase === 'latency'
    },
    {
      label: '抖动 ms',
      value: jit > 0 ? fmt(jit, 1) : props.running ? '…' : '—',
      active: false
    }
  ]
})
</script>

<style scoped>
.result-area {
  margin-bottom: var(--ph-space-4);
}
.phase-line {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-3);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.phase-tag {
  color: var(--ph-primary);
  font-weight: 600;
}
.cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--ph-space-3);
}
.stat-card {
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius-lg);
  padding: var(--ph-space-4);
  text-align: center;
  background: var(--ph-bg-surface);
}
.stat-card.active {
  border-color: var(--ph-primary);
}
.stat-value {
  font-size: 28px;
  font-weight: 600;
  line-height: 1.2;
}
.stat-label {
  margin-top: var(--ph-space-1);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
