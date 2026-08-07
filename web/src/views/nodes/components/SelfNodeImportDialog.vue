<template>
  <el-dialog v-model="visible" title="一键导入节点" width="600px">
    <el-alert type="info" :closable="false" class="import-hint">
      支持导入 vless://、vmess://、ss://、trojan:// 格式的节点链接
    </el-alert>
    <el-input
      v-model="importUrl"
      type="textarea"
      :rows="6"
      placeholder="粘贴节点链接，例如：vless://uuid@server:port?..."
    />
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="onImport">导入</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { parseNodeUrl, type SelfNodeForm } from '../self-node-utils'

const visible = defineModel<boolean>({ required: true })

const emit = defineEmits<{
  (e: 'imported', parsed: Partial<SelfNodeForm>): void
}>()

const importUrl = ref('')

// 每次打开清空输入
watch(visible, (open) => {
  if (open) importUrl.value = ''
})

const onImport = () => {
  const url = importUrl.value.trim()
  if (!url) {
    ElMessage.warning('请输入节点链接')
    return
  }
  try {
    const parsed = parseNodeUrl(url)
    if (!parsed) {
      ElMessage.error('无法解析节点链接，请检查格式')
      return
    }
    emit('imported', parsed)
    ElMessage.success('节点已导入，请检查并保存')
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '导入失败')
  }
}
</script>

<style scoped>
.import-hint {
  margin-bottom: var(--ph-space-4);
}
</style>
