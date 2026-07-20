<template>
  <!-- 多地域测速段(体检第二段):8 区串行,实时逐行到达;历史报告一次性给全量。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">多地域测速</span>
      <span v-if="subText" class="exam-section-sub">{{ subText }}</span>
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
    <div v-else class="exam-region-empty">{{ emptyText }}</div>
  </section>
</template>

<script setup lang="ts">
import type { ExamRegionResult } from '@/types'
import { regionRowStatus, regionStatusText, formatTtfb, formatMbps } from './regionspeed'

withDefaults(
  defineProps<{
    regions: ExamRegionResult[]
    subText?: string
    emptyText?: string
  }>(),
  { subText: '', emptyText: '等待多地域测速…' }
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
