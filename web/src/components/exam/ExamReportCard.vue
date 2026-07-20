<template>
  <!-- 历史报告卡:由一份静态 ExamReport 渲染,与实时体检对话框(NodeExamDialog)复用同一双栏布局与段组件。
       终态呈现:无进行中高亮,缺数据项以「—」占位;稳定性无原始采样序列 -> 不画 sparkline。 -->
  <div class="exam-report-card">
    <ExamReportLayout
      :stability="report.stability ?? null"
      :show-sparkline="false"
      :regions="report.region_speed?.regions ?? []"
      :unlocks="report.unlock?.results ?? []"
      :egress="report.egress ?? null"
      terminal
    />
  </div>
</template>

<script setup lang="ts">
import type { ExamReport } from '@/types'
import ExamReportLayout from './ExamReportLayout.vue'

defineProps<{ report: ExamReport }>()
</script>

<style scoped>
.exam-report-card {
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-lg);
}
</style>
