<template>
  <!-- 体检双栏布局(左:出网信息 + 稳定性;右:多地域 + 解锁)。出网为新段序第一段,置左栏顶部。
       总分评分环置于左栏顶部出网段之上。
       实时体检对话框(NodeExamDialog)与历史报告卡(ExamReportCard)同构复用此布局:
       宽容器双栏并排,窄容器(历史抽屉)自动收成单栏。 -->
  <div class="exam-layout">
    <div class="exam-col">
      <OverallScoreSection :report="overallReport" :terminal="terminal" />
      <EgressSection :egress="egress" :active="egressActive" :terminal="terminal" />
      <StabilitySection
        :metrics="stability"
        :samples="samples"
        :show-sparkline="showSparkline"
        :error="stabilityError"
      />
    </div>
    <div class="exam-col">
      <RegionSpeedSection :regions="regions" :active="regionActive" :terminal="terminal" />
      <UnlockSection :results="unlocks" :active="unlockActive" :terminal="terminal" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type {
  ExamStabilityMetrics,
  ExamStabilitySample,
  ExamRegionResult,
  ExamUnlockResult,
  ExamEgressMetrics,
  ExamReport
} from '@/types'
import OverallScoreSection from './OverallScoreSection.vue'
import StabilitySection from './StabilitySection.vue'
import EgressSection from './EgressSection.vue'
import RegionSpeedSection from './RegionSpeedSection.vue'
import UnlockSection from './UnlockSection.vue'

const props = withDefaults(
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

// 构造完整 ExamReport 供总分计算
const overallReport = computed((): ExamReport => {
  return {
    stability: props.stability ?? undefined,
    region_speed: props.regions.length > 0 ? { regions: props.regions } : undefined,
    unlock: props.unlocks.length > 0 ? { results: props.unlocks } : undefined,
    egress: props.egress ?? undefined
  }
})
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
