<template>
  <el-dropdown
    :disabled="disabled"
    trigger="click"
    @command="(cmd: VersionCommand) => emit('command', cmd)"
    @visible-change="(visible: boolean) => emit('visible-change', visible)"
  >
    <el-button :disabled="disabled">
      历史
      <el-icon class="el-icon--right"><ArrowDown /></el-icon>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu v-loading="versionsLoading">
        <el-dropdown-item v-if="versions.length === 0" disabled>暂无历史版本</el-dropdown-item>
        <el-dropdown-item
          v-for="ver in versions"
          :key="ver.version"
          :command="{ action: 'preview', version: ver.version }"
          :disabled="previewingVersion === ver.version"
        >
          <div class="version-item">
            <span>版本 {{ ver.version }}</span>
            <span class="version-time">{{ formatTime(ver.created_at) }}</span>
            <el-tag v-if="ver.version === currentVersion" type="success" size="small">当前</el-tag>
          </div>
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup lang="ts">
import { ArrowDown } from '@element-plus/icons-vue'
import type { TemplateVersion } from '@/api/templates'

export interface VersionCommand {
  action: string
  version: number
}

interface Props {
  versions: TemplateVersion[]
  versionsLoading: boolean
  previewingVersion: number | null
  currentVersion: number | null
  disabled: boolean
}

defineProps<Props>()

const emit = defineEmits<{
  (e: 'command', command: VersionCommand): void
  (e: 'visible-change', visible: boolean): void
}>()

// Format time as HH:mm:ss
function formatTime(isoString: string): string {
  const date = new Date(isoString)
  return date.toLocaleTimeString('zh-CN', { hour12: false })
}
</script>

<style scoped>
.version-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  min-width: 200px;
}
.version-time {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-left: auto;
}
</style>
