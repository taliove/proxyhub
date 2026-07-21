<template>
  <!-- 分享卡对话框:承载海报(ExamShareCard)之外的全部控件 —— 节点名开关、IP 开关、下载 PNG、复制到剪贴板。
       海报本身零按钮以免被截进图;渲染失败给显式 error 提示,不静默。 -->
  <el-dialog
    v-model="visible"
    :title="`分享体检 · ${nodeLabel}`"
    width="480px"
    class="share-dialog"
  >
    <div class="share-toolbar">
      <el-switch v-model="showFullName" size="small" active-text="显示完整节点名" />
      <el-switch v-model="showIp" size="small" active-text="显示 IP" />
    </div>

    <el-alert
      v-if="errorMsg"
      class="share-alert"
      type="error"
      :title="errorMsg"
      show-icon
      :closable="false"
    />

    <div class="share-stage">
      <ExamShareCard
        v-if="report"
        ref="cardRef"
        :report="report"
        :node-name="nodeName"
        :exam-time="examTime"
        :masked="!showFullName"
        :show-ip="showIp"
      />
    </div>

    <template #footer>
      <el-button :loading="copying" :disabled="!report" @click="onCopy">复制到剪贴板</el-button>
      <el-button type="primary" :loading="downloading" :disabled="!report" @click="onDownload">
        下载 PNG
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { toBlob } from 'html-to-image'
import type { ExamReport } from '@/types'
import ExamShareCard from './ExamShareCard.vue'
import { displayNodeName } from './sharecard'

const props = withDefaults(
  defineProps<{
    report: ExamReport | null
    nodeName?: string
    examTime?: string | number | Date
  }>(),
  { nodeName: '', examTime: '' }
)

const visible = defineModel<boolean>('visible', { required: true })

const showFullName = ref(false)
const showIp = ref(false)
const downloading = ref(false)
const copying = ref(false)
const errorMsg = ref('')

const cardRef = ref<InstanceType<typeof ExamShareCard> | null>(null)

// 标题用打码名(与海报默认一致,避免在标题栏泄露完整名)。
const nodeLabel = computed(() => displayNodeName(props.nodeName, true))

const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

// captureBlob 把海报 DOM 渲染为 PNG blob;失败抛错交由调用方统一提示。
const captureBlob = async (): Promise<Blob> => {
  const el = cardRef.value?.$el as HTMLElement | undefined
  if (!el) throw new Error('分享卡尚未就绪')
  const blob = await toBlob(el, {
    pixelRatio: 2,
    cacheBust: true,
    backgroundColor: cssVar('--ph-bg-surface') || '#ffffff'
  })
  if (!blob) throw new Error('图片生成失败')
  return blob
}

const fileName = () => `proxyhub-exam-${Date.now()}.png`

const onDownload = async () => {
  if (downloading.value) return
  downloading.value = true
  errorMsg.value = ''
  try {
    const blob = await captureBlob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = fileName()
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('已下载分享卡')
  } catch (err) {
    errorMsg.value = `生成 PNG 失败:${(err as Error).message || '未知错误'}`
  } finally {
    downloading.value = false
  }
}

// clipboardSupported 运行环境是否支持写图片到剪贴板(Safari/权限受限时为 false)。
const clipboardSupported = () =>
  typeof ClipboardItem !== 'undefined' && !!navigator.clipboard?.write

const onCopy = async () => {
  if (copying.value) return
  if (!clipboardSupported()) {
    errorMsg.value = '当前浏览器不支持复制图片,请改用下载 PNG'
    return
  }
  copying.value = true
  errorMsg.value = ''
  try {
    const blob = await captureBlob()
    await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })])
    ElMessage.success('已复制到剪贴板')
  } catch (err) {
    errorMsg.value = `复制失败:${(err as Error).message || '未知错误'}`
  } finally {
    copying.value = false
  }
}
</script>

<style scoped>
.share-toolbar {
  display: flex;
  justify-content: flex-end;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-3);
}
.share-alert {
  margin-bottom: var(--ph-space-3);
}
.share-stage {
  display: flex;
  justify-content: center;
  padding: var(--ph-space-2) 0;
}
</style>
