<template>
  <!-- 完整三段报告卡(稳定性 + 多地域测速 + 解锁),由一份静态 ExamReport 渲染。
       与实时体检对话框(NodeExamDialog)复用同一批段组件与纯函数,无双份实现。
       注:历史报告只持久化聚合指标,不含稳定性原始采样序列 -> 不画 sparkline。 -->
  <div class="exam-report-card">
    <StabilitySection :metrics="report.stability ?? null" :show-sparkline="false" />
    <RegionSpeedSection
      :regions="report.region_speed?.regions ?? []"
      empty-text="本次体检无多地域测速数据"
    />
    <UnlockSection :results="report.unlock?.results ?? []" phase-text="" />
  </div>
</template>

<script setup lang="ts">
import type { ExamReport } from '@/types'
import StabilitySection from './StabilitySection.vue'
import RegionSpeedSection from './RegionSpeedSection.vue'
import UnlockSection from './UnlockSection.vue'

defineProps<{ report: ExamReport }>()
</script>

<style scoped>
.exam-report-card {
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-lg);
}
</style>
