<template>
  <span class="row-ops" @click.stop>
    <el-dropdown
      size="small"
      trigger="click"
      @command="(cmd: TestCommand) => emit('test', row, cmd)"
    >
      <el-button link type="primary">
        检查
        <el-icon><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <!-- 4 个检查动作,与批量面同名同义(见 CONTEXT「检查动作」)。 -->
          <el-dropdown-item command="detect" :disabled="detecting">
            {{ detecting ? '出网快速检测(进行中)' : '出网快速检测' }}
          </el-dropdown-item>
          <el-dropdown-item command="stability">出网+稳定性</el-dropdown-item>
          <el-dropdown-item command="speedtest">快速测速</el-dropdown-item>
          <el-dropdown-item command="exam">
            {{ runningExamKeys.has(row.node_key) ? '深度体检(查看进度)' : '深度体检' }}
          </el-dropdown-item>
          <!-- 本机实测:浏览器端验收测量,跳转独立页并预填标注(ticket 0034) -->
          <el-dropdown-item command="client-speedtest" divided>本机实测</el-dropdown-item>
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

// 节点行「检查/分享」单元格:检查下拉 4 动作(出网快速检测 / 出网+稳定性 / 快速测速 / 深度体检)
// 与批量面同名同义,外加本机实测(独立页)。detecting 为全页共享的解锁检测运行态
// (batch_detection 全局单例):进行中时"出网快速检测"项标注进行中并禁用。
// runningExamKeys 用于深度体检项显示"查看进度"。
defineProps<{
  row: UnifiedNode
  detecting: boolean
  runningExamKeys: Set<string>
}>()

const emit = defineEmits<{
  (e: 'test', row: UnifiedNode, cmd: TestCommand): void
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
