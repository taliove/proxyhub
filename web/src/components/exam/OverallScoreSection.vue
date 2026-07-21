<template>
  <!-- 体检总分段:大评分环(总分 + 档位 + 随档位配色)+ 部分数据标注 + 不可信标记。
       与稳定性分区的小环有视觉层级区分(更大、标题更醒目)。 -->
  <section class="overall-score-section">
    <div class="overall-score-header">
      <h3 class="overall-score-title">综合评分</h3>
      <div class="overall-score-badges">
        <span v-if="scoreResult.unreliable" class="overall-score-unreliable">不可信</span>
        <span v-else-if="scoreResult.partial" class="overall-score-partial">部分数据</span>
      </div>
    </div>
    <div class="overall-score-body">
      <div class="overall-score-ring-wrap">
        <svg class="overall-score-ring" viewBox="0 0 160 160" aria-hidden="true">
          <circle class="overall-score-ring-track" cx="80" cy="80" r="70" />
          <circle
            class="overall-score-ring-arc"
            cx="80"
            cy="80"
            r="70"
            :stroke="scoreColor"
            :stroke-dasharray="RING_CIRC"
            :stroke-dashoffset="ringOffset"
          />
        </svg>
        <div class="overall-score-ring-center">
          <div class="overall-score-ring-score" :style="{ color: scoreColor }">
            {{ displayScore }}
          </div>
          <div class="overall-score-ring-label">{{ gradeText }}</div>
        </div>
      </div>
      <div v-if="!terminal" class="overall-score-hint">体检完成后更新</div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamReport } from '@/types'
import { calculateExamScore, gradeLabel, gradeColorVar } from './score'

const props = withDefaults(
  defineProps<{
    report: ExamReport
    terminal?: boolean
  }>(),
  { terminal: false }
)

const RING_CIRC = 2 * Math.PI * 70

const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

// 进行中(terminal=false)用 progressive:缺段计 0,分数由小到大爬升;
// 完成态 / 历史报告卡(terminal=true)用 normalized:就已有维度给出公允满量程分。
const scoreResult = computed(() =>
  calculateExamScore(props.report, props.terminal ? 'normalized' : 'progressive')
)
const scoreColor = computed(() => cssVar(gradeColorVar(scoreResult.value.grade)) || '#059669')
const gradeText = computed(() => gradeLabel(scoreResult.value.grade))
const ringOffset = computed(
  () => RING_CIRC * (1 - Math.max(0, Math.min(100, scoreResult.value.total)) / 100)
)

// 显示分数:进行中时显示渐进分数(随段到达增长),完成后显示最终分数。
// 不可信时显示 0。
const displayScore = computed(() => {
  if (scoreResult.value.unreliable) {
    return '0'
  }
  return Math.round(scoreResult.value.total).toString()
})
</script>

<style scoped>
.overall-score-section {
  margin-bottom: var(--ph-space-5);
  padding: var(--ph-space-4);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-lg);
}
.overall-score-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--ph-space-3);
}
.overall-score-title {
  margin: 0;
  font-size: var(--ph-text-lg);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.overall-score-badges {
  display: flex;
  gap: var(--ph-space-2);
}
.overall-score-partial {
  padding: 2px var(--ph-space-2);
  border-radius: var(--ph-radius-sm);
  background: var(--ph-bg-warning);
  color: var(--ph-warning);
  font-size: var(--ph-text-xs);
  font-weight: 500;
}
.overall-score-unreliable {
  padding: 2px var(--ph-space-2);
  border-radius: var(--ph-radius-sm);
  background: var(--ph-bg-danger);
  color: var(--ph-danger);
  font-size: var(--ph-text-xs);
  font-weight: 500;
}
.overall-score-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--ph-space-2);
}
.overall-score-ring-wrap {
  position: relative;
  width: 140px;
  height: 140px;
  flex: none;
}
.overall-score-ring {
  width: 140px;
  height: 140px;
  transform: rotate(-90deg);
}
.overall-score-ring-track {
  fill: none;
  stroke: var(--ph-border);
  stroke-width: 12;
}
.overall-score-ring-arc {
  fill: none;
  stroke-width: 12;
  stroke-linecap: round;
  transition: stroke-dashoffset 0.4s ease;
}
.overall-score-ring-center {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}
.overall-score-ring-score {
  font-size: var(--ph-text-display);
  font-weight: 700;
  line-height: 1;
}
.overall-score-ring-label {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.overall-score-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
  text-align: center;
}
</style>
