<template>
  <div class="event-list">
    <div v-for="ev in events" :key="ev.id" class="event-row">
      <el-icon class="event-icon"><component :is="levelIcon(ev.level)" /></el-icon>
      <span class="event-time">{{ formatTime(ev.created_at) }}</span>
      <span class="event-msg">{{ ev.message }}</span>
      <el-button v-if="showData && ev.data" link type="info" @click="emit('toggle-data', ev.id)">
        {{ openData[ev.id] ? '收起' : '数据' }}
      </el-button>
    </div>
    <pre v-if="dataText" class="event-data">{{ dataText }}</pre>
  </div>
</template>

<script setup lang="ts">
import type { RefreshEvent } from '@/types'
import { levelIcon, formatTime } from '../utils'

withDefaults(
  defineProps<{
    events: RefreshEvent[]
    showData?: boolean
    openData?: Record<number, boolean>
    dataText?: string
  }>(),
  { showData: false, openData: () => ({}), dataText: '' }
)

const emit = defineEmits<{ (e: 'toggle-data', id: number): void }>()
</script>

<style scoped>
.event-list {
  display: flex;
  flex-direction: column;
}
.event-row {
  display: flex;
  gap: var(--ph-space-2);
  padding: var(--ph-space-1) 0;
  align-items: flex-start;
}
.event-icon {
  margin-top: 2px;
}
.event-time {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  min-width: 70px;
}
.event-msg {
  flex: 1;
}
.event-data {
  background: var(--ph-bg-hover);
  padding: var(--ph-space-2);
  margin: var(--ph-space-1) 0 0 84px;
  font-size: var(--ph-text-xs);
  overflow-x: auto;
}
</style>
