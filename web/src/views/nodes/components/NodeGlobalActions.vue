<template>
  <el-dropdown trigger="click" @command="(cmd: string) => emit('command', cmd)">
    <el-button>
      全局操作
      <el-icon class="el-icon--right"><ArrowDown /></el-icon>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item command="detect-all" :disabled="detecting">检测全部</el-dropdown-item>
        <el-dropdown-item command="detect-filtered" :disabled="detecting">
          检测筛选结果
        </el-dropdown-item>
        <el-dropdown-item command="block-source">按机场屏蔽</el-dropdown-item>
        <el-dropdown-item command="purge-airport" divided :disabled="detecting">
          <span class="danger-item">清空机场节点</span>
        </el-dropdown-item>
        <el-dropdown-item command="cleanup" :disabled="detecting">
          <span class="danger-item">清理失败节点</span>
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup lang="ts">
import { ArrowDown } from '@element-plus/icons-vue'

// 页面级操作(与选中集无关)统一收进标题行下拉;清理是不可逆感操作,标危险色。
defineProps<{ detecting: boolean }>()

const emit = defineEmits<{
  (e: 'command', cmd: string): void
}>()
</script>

<style scoped>
.danger-item {
  color: var(--ph-danger);
}
</style>
