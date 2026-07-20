<template>
  <!-- 历史报告卡:由一份静态 ExamReport 渲染,与实时体检对话框(NodeExamDialog)复用同一双栏布局与段组件。
       终态呈现:无进行中高亮,缺数据项以「—」占位;稳定性无原始采样序列 -> 不画 sparkline。
       右上角「分享」把这份历史报告渲染成分享卡 PNG。 -->
  <div class="exam-report-card">
    <div class="exam-report-toolbar">
      <el-button link type="primary" size="small" @click="shareVisible = true">分享</el-button>
    </div>
    <ExamReportLayout
      :stability="report.stability ?? null"
      :show-sparkline="false"
      :regions="report.region_speed?.regions ?? []"
      :unlocks="report.unlock?.results ?? []"
      :egress="report.egress ?? null"
      terminal
    />

    <ExamShareDialog
      v-model:visible="shareVisible"
      :report="report"
      :node-name="nodeName"
      :exam-time="examTime"
    />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ExamReport } from '@/types'
import ExamReportLayout from './ExamReportLayout.vue'
import ExamShareDialog from './ExamShareDialog.vue'

withDefaults(
  defineProps<{
    report: ExamReport
    // 分享卡展示用:节点名(空则由分享卡回落占位)与体检时间(历史记录的 created_at)。
    nodeName?: string
    examTime?: string | number | Date
  }>(),
  { nodeName: '', examTime: '' }
)

const shareVisible = ref(false)
</script>

<style scoped>
.exam-report-card {
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-lg);
}
.exam-report-toolbar {
  display: flex;
  justify-content: flex-end;
}
</style>
