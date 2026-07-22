<template>
  <header class="ph-page-header">
    <div class="ph-page-header__text">
      <h1 class="ph-page-header__title">{{ title }}</h1>
      <p v-if="description || $slots.description" class="ph-page-header__desc">
        <slot name="description">{{ description }}</slot>
      </p>
    </div>
    <div v-if="$slots.default" class="ph-page-header__actions">
      <slot />
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const props = defineProps<{
  /** 页面标题,默认取当前路由 meta.title;仅确需与路由标题不一致时显式覆盖 */
  title?: string
  /** 可选一行描述;含富文本(如 code)时改用 #description 插槽 */
  description?: string
}>()

const route = useRoute()
const title = computed(() => props.title ?? (route.meta.title as string | undefined) ?? '')
</script>

<style scoped>
.ph-page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--ph-space-4);
  margin-bottom: var(--ph-space-4);
}

.ph-page-header__title {
  margin: 0;
  font-size: var(--ph-text-xl);
  font-weight: 600;
  line-height: 1.3;
  color: var(--ph-text-primary);
}

.ph-page-header__desc {
  margin: var(--ph-space-1) 0 0;
  font-size: var(--ph-text-sm);
  line-height: 1.5;
  color: var(--ph-text-secondary);
}

.ph-page-header__actions {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  flex-shrink: 0;
}
</style>
