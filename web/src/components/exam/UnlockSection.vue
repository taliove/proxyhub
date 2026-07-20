<template>
  <!-- 解锁段:6 个目标从打开即全占位;数据到达即填档位/区域,正在检测的项高亮。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">解锁检测</span>
      <span class="exam-section-count">{{ settledCount }}/{{ rows.length }}</span>
    </header>

    <div class="exam-unlock-grid">
      <div
        v-for="r in rows"
        :key="r.name"
        class="exam-row exam-unlock-item"
        :class="`is-${r.status}`"
      >
        <span class="exam-unlock-target">{{ r.name }}</span>
        <span v-if="r.result" class="exam-unlock-badges">
          <span class="exam-unlock-level" :style="{ color: unlockColor(r.result) }">
            {{ unlockLabel(r.result) }}
          </span>
          <span v-if="regionBadge(r.result)" class="exam-unlock-region">{{
            regionBadge(r.result)
          }}</span>
        </span>
        <span v-else class="exam-unlock-pending">{{ pendingText(r.status) }}</span>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamUnlockResult } from '@/types'
import { buildUnlockRows, type UnlockRow, type RowStatus } from './examrows'
import { unlockLabel, unlockColorVar, regionBadge } from './unlock'

const props = withDefaults(
  defineProps<{
    results: ExamUnlockResult[]
    active?: boolean
    terminal?: boolean
  }>(),
  { active: false, terminal: false }
)

const rows = computed<UnlockRow[]>(() => buildUnlockRows(props.results, props.active))
const settledCount = computed(() => rows.value.filter((r) => r.result !== null).length)

// 解锁档位颜色:令牌变量取 CSS 实际值(随亮/暗主题),缺失兜底中性灰。
const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()
const unlockColor = (r: ExamUnlockResult) => cssVar(unlockColorVar(r)) || '#64748b'

const pendingText = (status: RowStatus): string =>
  status === 'active' ? '检测中' : props.terminal ? '—' : '等待'
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
.exam-unlock-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 0 var(--ph-space-4);
}
.exam-unlock-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ph-space-2);
  padding: var(--ph-space-1) var(--ph-space-2);
  border-bottom: 1px solid var(--ph-border-light);
  font-size: var(--ph-text-sm);
}
.exam-unlock-target {
  color: var(--ph-text-secondary);
}
.exam-unlock-badges {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.exam-unlock-level {
  font-weight: 600;
}
.exam-unlock-region {
  padding: 0 var(--ph-space-2);
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius-full);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-unlock-pending {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
/* 未到达项压暗;正在检测项脉冲高亮。 */
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
