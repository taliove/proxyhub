<template>
  <el-dropdown trigger="click" @command="(full: boolean) => emit('test', full)">
    <el-button link type="primary">
      测试
      <el-icon><ArrowDown /></el-icon>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item :command="false">抽样测试</el-dropdown-item>
        <el-dropdown-item :command="true">测全部</el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup lang="ts">
import { ArrowDown } from '@element-plus/icons-vue'

// 机场列表行内「测试」下拉(ticket 0042,自 Airports.vue 抽取瘦身):
// 抽样测试/测全部两项,只上抛意图(full 参数),由父级接线运行模式对话框;
// 禁用机场同样可测(对齐 0037 已放开的语义)。
const emit = defineEmits<{
  // full=false 抽样测试;full=true 测全部
  (e: 'test', full: boolean): void
}>()
</script>
