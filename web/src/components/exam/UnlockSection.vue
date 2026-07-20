<template>
  <!-- 解锁区(体检第三段):6 个目标并行判定,随 SSE 逐个到达。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">解锁检测</span>
      <span class="exam-section-sub">{{ phaseText }}</span>
    </header>

    <div v-if="results.length" class="exam-unlock-grid">
      <div v-for="r in results" :key="r.target_name" class="exam-unlock-item">
        <span class="exam-unlock-target">{{ r.target_name }}</span>
        <span class="exam-unlock-badges">
          <span class="exam-unlock-level" :style="{ color: unlockColor(r) }">
            {{ unlockLabel(r) }}
          </span>
          <span v-if="regionBadge(r)" class="exam-unlock-region">{{ regionBadge(r) }}</span>
        </span>
      </div>
    </div>
    <div v-else class="exam-unlock-empty">等待解锁检测…</div>
  </section>
</template>

<script setup lang="ts">
import type { ExamUnlockResult } from '@/types'
import { unlockLabel, unlockColorVar, regionBadge } from './unlock'

defineProps<{
  results: ExamUnlockResult[]
  phaseText: string
}>()

// 解锁档位颜色:令牌变量取 CSS 实际值(随亮/暗主题),缺失兜底中性灰。
const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()
const unlockColor = (r: ExamUnlockResult) => cssVar(unlockColorVar(r)) || '#64748b'
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
.exam-unlock-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--ph-space-2) var(--ph-space-4);
}
.exam-unlock-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ph-space-2);
  padding: var(--ph-space-2) 0;
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
.exam-unlock-empty {
  padding: var(--ph-space-4);
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
</style>
