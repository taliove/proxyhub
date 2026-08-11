<template>
  <!-- 迁移待处理冲突(从 SlotManager 抽取,400 行门禁):同名竞争落选行,
       认领(改成槽位)或放弃(清除覆盖) -->
  <div class="conflict-zone">
    <div class="conflict-title">待处理冲突（旧名称覆盖同名竞争落选）</div>
    <div v-for="c in conflicts" :key="c.node_key" class="conflict-row">
      <span class="conflict-name">{{ c.display_name }}</span>
      <span class="muted">{{ c.node_key }}</span>
      <span class="conflict-ops">
        <el-button link type="primary" size="small" @click="emit('claim', c)">认领为槽位</el-button>
        <el-button link type="danger" size="small" @click="drop(c)">放弃</el-button>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import client from '@/api/client'
import { type SlotConflictRow } from '@/api/slots'
import { apiErrorMessage } from '../utils'

defineProps<{ conflicts: SlotConflictRow[] }>()

const emit = defineEmits<{
  // claim 交给父级(SlotManager 的编辑器对话框带预览/变量)
  (e: 'claim', c: SlotConflictRow): void
  (e: 'changed'): void
}>()

// 放弃:清掉覆盖行(display_name 残留随行的去留规则清除,收藏保留)
const drop = async (c: SlotConflictRow) => {
  try {
    await ElMessageBox.confirm(
      `放弃节点 ${c.node_key} 的旧名称「${c.display_name}」？该名称仍被别的节点占用。`,
      '放弃覆盖',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await client.delete('/nodes/override', { data: { node_key: c.node_key } })
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '清理覆盖失败'))
    return
  }
  ElMessage.success('已放弃')
  emit('changed')
}
</script>

<style scoped>
.conflict-zone {
  margin-top: var(--ph-space-3);
  border: 1px dashed var(--ph-warning);
  border-radius: var(--ph-radius-sm);
  padding: var(--ph-space-2) var(--ph-space-3);
}
.conflict-title {
  font-size: var(--ph-text-sm);
  color: var(--ph-warning);
  margin-bottom: var(--ph-space-2);
}
.conflict-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-1) 0;
}
.conflict-name {
  font-weight: 500;
}
.conflict-ops {
  margin-left: auto;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
