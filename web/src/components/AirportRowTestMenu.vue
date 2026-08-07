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

<style scoped>
/* ticket 0046:el-dropdown 包装使 EP `.el-button + .el-button` 兄弟选择器不命中,
   丢失 12px 间距；且 .el-dropdown 默认 vertical-align 造成基线偏移。
   补齐后与旁边「详情/刷新」裸 link 按钮静止/hover 观感一致 */
.el-dropdown {
  margin-left: 12px;
  vertical-align: middle;
}
</style>
