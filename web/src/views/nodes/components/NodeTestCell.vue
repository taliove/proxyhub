<template>
  <span class="row-ops" @click.stop>
    <el-dropdown
      size="small"
      trigger="click"
      @command="(mode: TestCommand) => emit('test', row, mode)"
    >
      <el-button link type="primary" :disabled="testing">
        测试
        <el-icon><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="quick">快测</el-dropdown-item>
          <el-dropdown-item command="real">真实检测</el-dropdown-item>
          <el-dropdown-item command="bandwidth">带宽测试</el-dropdown-item>
          <el-dropdown-item command="exam" divided>
            {{ runningExamKeys.has(row.node_key) ? '查看进度' : '深度体检' }}
          </el-dropdown-item>
          <!-- 本机实测:浏览器端验收测量,跳转独立页并预填标注(ticket 0034) -->
          <el-dropdown-item command="speedtest" divided>本机实测</el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>
    <el-button
      link
      type="primary"
      :icon="DocumentCopy"
      title="复制链接"
      :disabled="!canShare(row)"
      @click="emit('copy-link', row)"
    />
    <el-button
      link
      type="primary"
      :icon="Grid"
      title="二维码"
      :disabled="!canShare(row)"
      @click="emit('show-qr', row)"
    />
  </span>
</template>

<script setup lang="ts">
import { ArrowDown, DocumentCopy, Grid } from '@element-plus/icons-vue'
import { canShare, type TestCommand } from './node-table-utils'
import type { UnifiedNode } from '../selfmerge'

// 节点行「测试/分享」单元格:从 NodeTable 抽出(400 行门禁),
// 行为与原版一致,新增"本机实测"跳转指令。
defineProps<{
  row: UnifiedNode
  testing: boolean
  runningExamKeys: Set<string>
}>()

const emit = defineEmits<{
  (e: 'test', row: UnifiedNode, mode: TestCommand): void
  (e: 'copy-link', row: UnifiedNode): void
  (e: 'show-qr', row: UnifiedNode): void
}>()
</script>

<style scoped>
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
</style>
