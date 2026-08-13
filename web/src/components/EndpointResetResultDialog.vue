<template>
  <!-- 重置成功弹窗(issue #117):展示新链接 + 一键复制 + 二维码;
       旧链接默认 3 天宽限,文案写清后果 -->
  <el-dialog v-model="visible" title="订阅链接已重置" width="560px">
    <p class="reset-hint">
      新链接已生成，原端点的筛选、精选、模板等配置全部保留。旧链接在宽限期（至
      {{ endpoint?.grace_expires_at }} UTC）内仍可使用，请尽快把各设备更新为新链接。
    </p>
    <el-input :value="url" readonly>
      <template #append>
        <el-button @click="copy">复制</el-button>
      </template>
    </el-input>
    <template #footer>
      <el-button @click="emit('qrcode', url)">二维码</el-button>
      <el-button type="primary" @click="visible = false">完成</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import type { Endpoint } from '@/types'
import { copyText } from '@/utils/clipboard'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  endpoint: Endpoint | null
  // 完整新订阅 URL(由父级按 origin+path+token 拼好)
  url: string
}>()

const emit = defineEmits<{
  (e: 'qrcode', url: string): void
}>()

// props.url 已覆盖 endpoint 全量信息,copy 走降级剪贴板
const copy = async () => {
  await copyText(props.url)
  ElMessage.success('新链接已复制到剪贴板')
}
</script>

<style scoped>
.reset-hint {
  margin: 0 0 var(--ph-space-3);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  line-height: 1.6;
}
</style>
