<template>
  <!-- 体检双栏布局(左:稳定性 + 出网信息;右:多地域 + 解锁)。
       实时体检对话框(NodeExamDialog)与历史报告卡(ExamReportCard)同构复用此布局:
       宽容器双栏并排,窄容器(历史抽屉)自动收成单栏。 -->
  <div class="exam-layout">
    <div class="exam-col">
      <StabilitySection
        :metrics="stability"
        :samples="samples"
        :show-sparkline="showSparkline"
        :error="stabilityError"
      />
      <EgressSection :egress="egress" :active="egressActive" :terminal="terminal" />
    </div>
    <div class="exam-col">
      <RegionSpeedSection :regions="regions" :active="regionActive" :terminal="terminal" />
      <UnlockSection :results="unlocks" :active="unlockActive" :terminal="terminal" />
    </div>
  </div>
</template>

<script setup lang="ts">
import type {
  ExamStabilityMetrics,
  ExamStabilitySample,
  ExamRegionResult,
  ExamUnlockResult,
  ExamEgressMetrics
} from '@/types'
import StabilitySection from './StabilitySection.vue'
import EgressSection from './EgressSection.vue'
import RegionSpeedSection from './RegionSpeedSection.vue'
import UnlockSection from './UnlockSection.vue'

withDefaults(
  defineProps<{
    stability: ExamStabilityMetrics | null
    samples?: ExamStabilitySample[]
    showSparkline?: boolean
    stabilityError?: string
    regions: ExamRegionResult[]
    regionActive?: boolean
    unlocks: ExamUnlockResult[]
    unlockActive?: boolean
    egress: ExamEgressMetrics | null
    egressActive?: boolean
    // terminal:体检已收口,各段未到达项以「—」呈现且不再高亮。
    terminal?: boolean
  }>(),
  {
    samples: () => [],
    showSparkline: false,
    stabilityError: '',
    regionActive: false,
    unlockActive: false,
    egressActive: false,
    terminal: false
  }
)
</script>

<style scoped>
.exam-layout {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
  gap: 0 var(--ph-space-6);
  align-items: start;
}
.exam-col {
  min-width: 0;
}
</style>
