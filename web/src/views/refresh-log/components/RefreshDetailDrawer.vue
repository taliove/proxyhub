<template>
  <el-drawer v-model="visible" title="刷新详情" size="640px">
    <template v-if="run">
      <el-descriptions :column="2" border size="small" class="drawer-block">
        <el-descriptions-item label="时间">{{ formatTime(run.started_at) }}</el-descriptions-item>
        <el-descriptions-item label="触发">{{ triggerLabel(run.trigger) }}</el-descriptions-item>
        <el-descriptions-item label="结果">
          <el-tag :type="statusTagType(run.status)" size="small">{{
            statusLabel(run.status)
          }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="节点数">{{ formatNodes(run) }}</el-descriptions-item>
      </el-descriptions>

      <div class="drawer-block">
        <el-alert v-if="error" type="error" :closable="false" show-icon>
          事件加载失败:{{ error }}
        </el-alert>
        <el-skeleton v-else-if="!events" :rows="3" animated />
        <div v-else>
          <div v-for="group in groupedEvents(events)" :key="group.stage" class="stage-block">
            <div class="stage-title">{{ stageLabel(group.stage) }}</div>
            <RefreshEventList
              :events="group.events"
              show-data
              :open-data="openData"
              :data-text="dataText(group.events)"
              @toggle-data="(id) => emit('toggle-data', id)"
            />
          </div>
        </div>
      </div>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import type { RefreshRun, RefreshEvent } from '@/types'
import RefreshEventList from './RefreshEventList.vue'
import {
  formatTime,
  formatNodes,
  triggerLabel,
  statusLabel,
  statusTagType,
  stageLabel,
  groupedEvents
} from '../utils'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  run: RefreshRun | null
  events: RefreshEvent[] | undefined
  error: string | undefined
  openData: Record<number, boolean>
}>()

const emit = defineEmits<{ (e: 'toggle-data', id: number): void }>()

// 某阶段内所有已展开事件的数据拼接文本
const dataText = (events: RefreshEvent[]): string => {
  const lines: string[] = []
  for (const e of events) {
    if (e.data && props.openData[e.id]) {
      try {
        lines.push(`${formatTime(e.created_at)}: ${JSON.stringify(JSON.parse(e.data), null, 2)}`)
      } catch {
        lines.push(`${formatTime(e.created_at)}: ${e.data}`)
      }
    }
  }
  return lines.join('\n')
}
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.stage-block {
  margin-bottom: var(--ph-space-4);
}
.stage-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
  color: var(--ph-text-regular);
}
</style>
