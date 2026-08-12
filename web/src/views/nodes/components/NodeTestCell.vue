<template>
  <span class="row-ops" @click.stop>
    <!-- 主入口「补齐信息」= batch_exam backfill(轻量);「高级」含三个子集动作与完整深度体检 -->
    <el-button link type="primary" @click="emit('test', row, 'backfill')">
      {{ runningExamKeys.has(row.node_key) ? '补齐信息（查看进度）' : '补齐信息' }}
    </el-button>
    <el-dropdown
      size="small"
      trigger="click"
      @command="(cmd: TestCommand) => emit('test', row, cmd)"
    >
      <el-button link>
        高级
        <el-icon><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="detect" :disabled="detecting">
            {{ detecting ? '出网快速检测（进行中）' : '出网快速检测' }}
          </el-dropdown-item>
          <el-dropdown-item command="stability">出网+稳定性</el-dropdown-item>
          <el-dropdown-item command="speedtest">快速测速</el-dropdown-item>
          <el-dropdown-item command="exam" divided>深度体检</el-dropdown-item>
          <!-- 本机实测:浏览器端验收测量,跳转独立页并预填标注(ticket 0034) -->
          <el-dropdown-item command="client-speedtest">本机实测</el-dropdown-item>
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

// 节点行「检查/分享」单元格:主入口「补齐信息」(= 深度体检 batch_exam mode=full,
// 出网/稳定性/解锁/测速/标签/地区回写一次产出);「高级」下拉保留三个子集动作
// (出网快速检测 / 出网+稳定性 / 快速测速)与本机实测(独立页),与批量面同名同义。
// detecting 为全页共享的解锁检测运行态(batch_detection 全局单例);
// runningExamKeys 命中时主入口显示"查看进度"。
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
