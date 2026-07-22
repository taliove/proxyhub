<template>
  <!-- 状态色点:克制的健康/状态指示,只消费语义色令牌。
       tone 语义:success 可用 / warning 可逆警示 / danger 不可用或不可逆 / info 中性 / muted 未测或禁用。
       label 同时作 tooltip 与 aria-label;不传则纯装饰(aria-hidden)。 -->
  <span
    class="status-dot"
    :class="`is-${tone}`"
    :title="label || undefined"
    role="img"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : true"
  />
</template>

<script setup lang="ts">
export type StatusDotTone = 'success' | 'warning' | 'danger' | 'info' | 'muted'

withDefaults(defineProps<{ tone: StatusDotTone; label?: string }>(), { label: '' })
</script>

<style scoped>
.status-dot {
  display: inline-block;
  width: var(--ph-space-2);
  height: var(--ph-space-2);
  border-radius: var(--ph-radius-full);
  flex: none;
}
.is-success {
  background: var(--ph-success);
}
.is-warning {
  background: var(--ph-warning);
}
.is-danger {
  background: var(--ph-danger);
}
.is-info {
  background: var(--ph-info);
}
.is-muted {
  background: var(--ph-text-placeholder);
}
</style>
