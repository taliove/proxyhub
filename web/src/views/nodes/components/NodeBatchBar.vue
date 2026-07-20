<template>
  <div class="batch-bar">
    <span class="muted">已选 {{ count }} 项</span>
    <el-button type="warning" size="small" @click="emit('block')">屏蔽选中</el-button>
    <el-button size="small" @click="emit('unblock')">取消屏蔽</el-button>
    <el-button type="primary" size="small" :disabled="detecting" @click="emit('detect')">
      检测选中
    </el-button>
    <el-button size="small" :disabled="examining || count === 0" @click="emit('exam')">
      批量体检
    </el-button>
    <span v-if="examining" class="muted exam-progress">
      体检中 {{ examCompleted }}/{{ examTotal }}
      <el-button link type="warning" size="small" @click="emit('cancel-exam')">取消</el-button>
    </span>
  </div>
</template>

<script setup lang="ts">
// 上下文批量栏:仅当选中数 > 0 时由装配层渲染,只承载针对选中集的操作。
// 屏蔽可逆(取消屏蔽存在),用 warning 不用 danger(危险色纪律)。
// 批量体检复用 jobs 轮询做轻量进度(完成 x/N,可取消),不接 SSE。
defineProps<{
  count: number
  detecting: boolean
  examining: boolean
  examCompleted: number
  examTotal: number
}>()

const emit = defineEmits<{
  (e: 'block'): void
  (e: 'unblock'): void
  (e: 'detect'): void
  (e: 'exam'): void
  (e: 'cancel-exam'): void
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
.exam-progress {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
</style>
