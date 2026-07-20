<template>
  <div class="batch-bar">
    <span class="muted">已选 {{ count }} 项</span>
    <el-button type="warning" size="small" @click="emit('block')">屏蔽选中</el-button>
    <el-button size="small" @click="emit('unblock')">取消屏蔽</el-button>
    <el-button type="primary" size="small" :disabled="detecting" @click="emit('detect')">
      检测选中
    </el-button>
  </div>
</template>

<script setup lang="ts">
// 上下文批量栏:仅当选中数 > 0 时由装配层渲染,只承载针对选中集的操作。
// 屏蔽可逆(取消屏蔽存在),用 warning 不用 danger(危险色纪律)。
defineProps<{
  count: number
  detecting: boolean
}>()

const emit = defineEmits<{
  (e: 'block'): void
  (e: 'unblock'): void
  (e: 'detect'): void
}>()
</script>

<style scoped>
.batch-bar {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  flex-wrap: wrap;
  margin-bottom: var(--ph-space-3);
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
