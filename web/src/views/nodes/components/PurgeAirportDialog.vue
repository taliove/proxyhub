<template>
  <el-dialog v-model="visible" title="清空机场节点" width="480px">
    <el-alert type="error" :closable="false" class="purge-warning">
      <template #title>将删除节点池中全部机场节点(自建节点不受影响)</template>
    </el-alert>
    <ul class="purge-notes">
      <li>节点将随下次刷新重新入池,清空后请手动刷新</li>
      <li>屏蔽名单与名称覆盖保留,节点重新入池后继续生效</li>
      <li>清空与刷新互斥:有刷新任务进行中时操作会被拒绝</li>
    </ul>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="danger" :loading="running" @click="confirmPurge">确认清空</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import client from '@/api/client'
import { apiErrorMessage } from '../utils'

// 一键清空机场节点(排障重置起点,CONTEXT.md「机场节点清空」):
// 内存池+DB 双清,自建豁免,屏蔽名单/名称覆盖保留;二次确认沿用 CleanupDialog 纪律。
const emit = defineEmits<{
  (e: 'done'): void
}>()

const visible = ref(false)
const running = ref(false)

const open = () => {
  visible.value = true
}

const confirmPurge = async () => {
  try {
    await ElMessageBox.confirm(
      '确认清空全部机场节点?节点将随下次刷新重新入池,屏蔽名单与名称覆盖保留。',
      '二次确认',
      { type: 'warning', confirmButtonText: '确认清空', cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  running.value = true
  try {
    const r = await client.post<unknown, { removed: number }>('/nodes/purge-airport', {})
    ElMessage.success(`已清空 ${r.removed ?? 0} 个机场节点,请手动刷新重新拉取`)
    visible.value = false
    emit('done')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '清空失败'))
  } finally {
    running.value = false
  }
}

defineExpose({ open })
</script>

<style scoped>
.purge-warning {
  margin-bottom: var(--ph-space-3);
}
.purge-notes {
  margin: 0;
  padding-left: var(--ph-space-4);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
  line-height: 1.8;
}
</style>
