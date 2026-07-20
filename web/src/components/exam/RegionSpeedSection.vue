<template>
  <!-- 多地域测速段:9 行(基准置顶 + 8 区域)从打开即全占位;数据到达即填值,正在测的行高亮。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">多地域测速</span>
      <span class="exam-section-count">{{ settledCount }}/{{ rows.length }}</span>
    </header>

    <table class="exam-region-table">
      <thead>
        <tr>
          <th>区域</th>
          <th>延迟</th>
          <th>下行</th>
          <th>状态</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="r in rows"
          :key="r.key"
          class="exam-row"
          :class="[`is-${r.status}`, { 'is-baseline': r.baseline }]"
        >
          <td class="exam-region-name">{{ r.name }}</td>
          <td>{{ ttfbText(r) }}</td>
          <td>{{ downText(r) }}</td>
          <td :class="`exam-region-status exam-region-status-${r.status}`">
            {{ statusText(r.status) }}
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamRegionResult } from '@/types'
import { buildRegionRows, type RegionRow, type RowStatus } from './examrows'
import { formatTtfb, formatMbps } from './regionspeed'

const props = withDefaults(
  defineProps<{
    regions: ExamRegionResult[]
    // active:本段是否进行中(驱动首个 waiting 行的高亮);历史报告卡传 false。
    active?: boolean
    // terminal:体检已收口,未到达行以「—」呈现而非「等待」。
    terminal?: boolean
  }>(),
  { active: false, terminal: false }
)

const rows = computed<RegionRow[]>(() => buildRegionRows(props.regions, props.active))
const settledCount = computed(
  () => rows.value.filter((r) => r.status === 'ok' || r.status === 'error').length
)

// 数值列:已结算给格式化值,失败给「-」,未到达给「—」。
const ttfbText = (r: RegionRow): string =>
  r.status === 'ok' ? formatTtfb(r.result?.ttfb_ms) : r.status === 'error' ? '-' : '—'
const downText = (r: RegionRow): string =>
  r.status === 'ok' ? formatMbps(r.result?.down_mbps) : r.status === 'error' ? '-' : '—'

const statusText = (status: RowStatus): string => {
  switch (status) {
    case 'ok':
      return '正常'
    case 'error':
      return '失败'
    case 'active':
      return '测速中'
    default:
      return props.terminal ? '—' : '等待'
  }
}
</script>

<style scoped>
.exam-section {
  padding: var(--ph-space-2) 0;
}
.exam-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: var(--ph-space-2);
}
.exam-section-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
}
.exam-section-count {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  font-variant-numeric: tabular-nums;
}
.exam-region-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--ph-text-sm);
  font-variant-numeric: tabular-nums;
}
.exam-region-table th,
.exam-region-table td {
  padding: var(--ph-space-1) var(--ph-space-2);
  text-align: left;
  border-bottom: 1px solid var(--ph-border-light);
}
.exam-region-table th {
  font-weight: 600;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-region-name {
  font-weight: 500;
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
.exam-region-status-active {
  color: var(--ph-color-primary);
}
.exam-region-status-waiting {
  color: var(--ph-text-secondary);
}
/* 基准行:置顶 + 左侧强调条 + 淡底,与区域行区分。 */
.exam-row.is-baseline {
  background: var(--ph-bg-hover);
}
.exam-row.is-baseline .exam-region-name {
  color: var(--ph-color-primary);
  box-shadow: inset 2px 0 0 var(--ph-color-primary);
  padding-left: var(--ph-space-2);
}
/* 未到达行整体压暗;正在处理行脉冲高亮。 */
.exam-row.is-waiting {
  opacity: 0.5;
}
.exam-row.is-active {
  animation: exam-pulse 1.2s ease-in-out infinite;
}
@keyframes exam-pulse {
  0%,
  100% {
    background: transparent;
  }
  50% {
    background: color-mix(in srgb, var(--ph-color-primary) 12%, transparent);
  }
}
</style>
