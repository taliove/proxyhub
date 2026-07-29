<template>
  <!-- 用量信息单元格(CONTEXT.md「用量信息」):剩余百分比 + 进度条 + 到期日;
       临期(<7 天)/已过期/流量将尽(<10%)标红;无数据 "-"。 -->
  <template v-if="percent !== null || expireLabel">
    <div v-if="percent !== null" class="usage-cell">
      <el-progress
        :percentage="percent"
        :stroke-width="8"
        :status="low ? 'exception' : undefined"
        class="usage-bar"
      />
      <span class="usage-text" :class="{ danger: low }">剩 {{ remainingText }}</span>
    </div>
    <div v-if="expireLabel" class="expire-text" :class="{ danger: expiringSoon }">
      {{ expireLabel }}
    </div>
  </template>
  <span v-else class="no-usage">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Airport } from '@/types'
import {
  formatBytes,
  usageRemaining,
  usageRemainingPercent,
  isUsageLow,
  expireText,
  isExpiringSoon,
  isExpired
} from '@/views/airport-utils'

const props = defineProps<{
  airport: Airport
}>()

const percent = computed(() => usageRemainingPercent(props.airport))
const remainingText = computed(() => formatBytes(usageRemaining(props.airport) ?? 0))
const low = computed(() => isUsageLow(props.airport))
const expiringSoon = computed(() => isExpiringSoon(props.airport))
const expireLabel = computed(() => {
  const text = expireText(props.airport)
  if (!text) return null
  return isExpired(props.airport) ? '已过期' : `${text} 到期`
})
</script>

<style scoped>
.usage-cell {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.usage-bar {
  flex: 1;
  min-width: 70px;
}
.usage-text {
  font-size: var(--ph-text-xs);
  white-space: nowrap;
}
.expire-text {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-top: var(--ph-space-1);
}
.danger {
  color: var(--el-color-danger);
}
.no-usage {
  color: var(--ph-text-secondary);
}
</style>
