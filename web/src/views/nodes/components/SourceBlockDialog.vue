<template>
  <el-dialog v-model="visible" title="按机场屏蔽" width="420px">
    <el-form label-width="90px">
      <el-form-item label="机场">
        <el-select v-model="source" placeholder="选择机场" clearable filterable class="ctl-full">
          <el-option v-for="src in sources" :key="src" :label="src" :value="src" />
        </el-select>
      </el-form-item>
      <el-alert
        type="info"
        :closable="false"
        title="作用于该机场当前的全部节点，下次生成订阅生效（刷新后依然保持）。"
      />
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :disabled="!source" @click="unblockSource">取消该机场屏蔽</el-button>
      <el-button type="warning" :disabled="!source" @click="blockSource">屏蔽该机场</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import client from '@/api/client'

// 按机场批量屏蔽:从全局操作下拉进入,作用于符合选择机场的整机场节点。
defineProps<{ sources: string[] }>()

const emit = defineEmits<{
  (e: 'done'): void
}>()

const visible = ref(false)
const source = ref('')

const open = () => {
  source.value = ''
  visible.value = true
}

const blockSource = async () => {
  const src = source.value
  try {
    await ElMessageBox.confirm(
      `将屏蔽机场「${src}」当前的全部节点，下次生成订阅生效（刷新后依然保持）。确认？`,
      '按机场批量屏蔽',
      { type: 'warning', confirmButtonText: '确认屏蔽', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  const res = await client.post<unknown, { count: number }>('/nodes/batch-block', { source: src })
  ElMessage.success(`已屏蔽机场「${src}」的 ${res.count} 个节点`)
  visible.value = false
  emit('done')
}

const unblockSource = async () => {
  const src = source.value
  const res = await client.post<unknown, { count: number }>('/nodes/batch-unblock', {
    source: src
  })
  ElMessage.success(`已取消机场「${src}」的 ${res.count} 个节点屏蔽`)
  visible.value = false
  emit('done')
}

defineExpose({ open })
</script>

<style scoped>
.ctl-full {
  width: 100%;
}
</style>
