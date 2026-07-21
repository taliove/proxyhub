<template>
  <el-dialog v-model="visible" :title="title" width="400px">
    <div v-if="loading" class="qr-loading">生成二维码中...</div>
    <div v-else-if="error" class="qr-error">
      <el-alert type="error" :closable="false">{{ error }}</el-alert>
    </div>
    <div v-else class="qr-content">
      <img v-if="dataUrl" :src="dataUrl" alt="二维码" class="qr-image" />
      <div class="qr-hint">{{ hint }}</div>
    </div>
    <template #footer>
      <el-button type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { generateQRCode } from '@/composables/useQRCode'

const visible = defineModel<boolean>({ required: true })

defineProps<{
  title: string
  hint: string
}>()

const loading = ref(false)
const error = ref('')
const dataUrl = ref('')

const show = async (content: string) => {
  visible.value = true
  loading.value = true
  error.value = ''
  dataUrl.value = ''

  try {
    dataUrl.value = await generateQRCode(content)
  } catch (err) {
    error.value = `生成二维码失败: ${err}`
  } finally {
    loading.value = false
  }
}

defineExpose({ show })

// Reset state when dialog closes
watch(visible, (isVisible) => {
  if (!isVisible) {
    dataUrl.value = ''
    error.value = ''
  }
})
</script>

<style scoped>
.qr-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: var(--ph-space-4) 0;
}
.qr-image {
  max-width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: var(--ph-radius-lg);
}
.qr-hint {
  margin-top: var(--ph-space-3);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.qr-loading,
.qr-error {
  text-align: center;
  padding: var(--ph-space-4) 0;
}
</style>
