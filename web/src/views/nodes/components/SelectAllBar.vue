<template>
  <div class="select-all-bar num">
    <template v-if="!allFiltered">
      已选当前页 {{ pageCount }} 条，
      <el-button link type="primary" @click="emit('enter')">
        点击选中全部 {{ total }} 条筛选结果
      </el-button>
    </template>
    <template v-else>
      已选中全部 {{ total }} 条筛选结果，
      <el-button link type="primary" @click="emit('exit')">点击取消</el-button>
    </template>
  </div>
</template>

<script setup lang="ts">
// Gmail 式提示条(issue #52):整页勾选且筛选结果多于一页时提供"选中全部筛选结果"入口;
// 进入作用域后常驻并转为取消。可见性与作用域状态由 useSelectAllFiltered 决定,本组件纯渲染。
defineProps<{
  allFiltered: boolean
  pageCount: number
  total: number
}>()

const emit = defineEmits<{
  (e: 'enter'): void
  (e: 'exit'): void
}>()
</script>

<style scoped>
.select-all-bar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--ph-space-1);
  margin-bottom: var(--ph-space-3);
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
