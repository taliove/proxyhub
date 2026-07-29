<template>
  <el-dialog
    v-model="visible"
    :title="`粘贴导入 - ${airport?.name ?? ''}`"
    width="640px"
    :close-on-click-modal="false"
    @closed="reset"
  >
    <template v-if="airport">
      <div class="guide">
        在机场面板导出订阅内容(通常为 base64 整段),粘贴到下方;也支持明文多行分享链接。
        系统不保存粘贴原文,导入即对该机场做一次单机场入池(同单机场刷新语义,不跑健康检查)。
        <template v-if="!isManual">
          <br />这是一次性导入:下次该机场 URL 刷新成功后,节点以 URL 拉取内容为准。
        </template>
      </div>
      <el-input
        v-model="content"
        type="textarea"
        :rows="8"
        placeholder="粘贴机场面板导出的订阅内容(base64 整段或明文多行 ss:// vless:// vmess:// trojan:// anytls://)"
        class="paste-box"
      />

      <!-- 用量手填仅手动机场可见;拉取型机场的用量由响应头自动捕获,无需手填 -->
      <template v-if="isManual">
        <div class="usage-title">用量信息(可选)</div>
        <el-form label-width="100px">
          <AirportUsageFields v-model="usage" />
        </el-form>
      </template>

      <!-- 有失败行时对话框停留:成功 N 条 + 失败明细(行号+原因) -->
      <div v-if="result" class="import-result">
        <el-alert
          :type="result.failures.length > 0 ? 'warning' : 'success'"
          :closable="false"
          :title="`成功导入 ${result.imported} 条`"
          :description="
            result.failures.length > 0 ? `${result.failures.length} 行解析失败` : undefined
          "
          show-icon
        />
        <el-table
          v-if="result.failures.length > 0"
          :data="result.failures"
          size="small"
          class="failure-table"
        >
          <el-table-column prop="line" label="行号" width="80" />
          <el-table-column prop="reason" label="原因" show-overflow-tooltip />
        </el-table>
      </div>
    </template>

    <template #footer>
      <el-button :type="hasFailures ? 'primary' : undefined" @click="visible = false">
        {{ result ? '完成' : '取消' }}
      </el-button>
      <el-button
        type="primary"
        :loading="importing"
        :disabled="!content.trim() || hasFailures"
        @click="doImport"
      >
        导入
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import client from '@/api/client'
import AirportUsageFields from '@/components/AirportUsageFields.vue'
import {
  usageFormFromAirport,
  usageFormToPayloadOrZero,
  type UsageFormValue
} from '@/views/airport-utils'

// 粘贴导入对话框(手动机场创建后引导/重新粘贴 + 拉取型机场一次性导入共用)。
// 凭证红线:粘贴内容只发本次请求,不落本地存储、不进日志。
const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  airport: Airport | null
}>()

const emit = defineEmits<{
  (e: 'imported', airport: Airport): void
}>()

const isManual = computed(() => props.airport?.source_type === 'manual')

const content = ref('')
const usage = ref<UsageFormValue>({
  remainingGb: null,
  totalGb: null,
  expireDate: '',
  webPageUrl: ''
})
const importing = ref(false)
const result = ref<{ imported: number; failures: { line: number; reason: string }[] } | null>(null)

// 有失败行:对话框停留展示明细,"导入"禁用,"完成"变主按钮
const hasFailures = computed(() => (result.value?.failures.length ?? 0) > 0)

// 内容变更即清上次结果:用户修正失败行后可直接重试,不必关窗重贴(Check LOW)
watch(content, () => {
  result.value = null
})

// 打开/切换机场时预填既有用量(编辑语义);粘贴内容永远从空开始(不保存原文)。
watch(
  () => [visible.value, props.airport?.id] as const,
  ([open]) => {
    if (open && props.airport) {
      usage.value = usageFormFromAirport(props.airport)
    }
  },
  { immediate: true }
)

const reset = () => {
  content.value = ''
  result.value = null
  importing.value = false
}

const doImport = async () => {
  if (!props.airport) return
  importing.value = true
  result.value = null
  try {
    // 用量仅手动机场随贴(拉取型由响应头捕获);全空也发零值(显式清空可达后端)
    const payload = isManual.value ? usageFormToPayloadOrZero(usage.value) : {}
    const resp = await client.post<
      unknown,
      { imported: number; failures: { line: number; reason: string }[] }
    >(`/airports/${props.airport.id}/import`, { content: content.value, ...payload })
    const failures = resp.failures ?? []
    emit('imported', props.airport)
    if (failures.length === 0) {
      // 全部成功:确认提示 + 自动关闭(列表/节点池视图由 imported 事件驱动刷新)
      ElMessage.success(`成功导入 ${resp.imported} 条`)
      visible.value = false
      return
    }
    // 有失败行:对话框停留,展示成功数与逐行明细
    result.value = { imported: resp.imported, failures }
  } catch (error) {
    const status = (error as { response?: { status?: number } })?.response?.status
    if (status === 409) {
      ElMessage.warning('与进行中的刷新或机场测试冲突,请稍候再试')
    } else if (status === 413) {
      ElMessage.error('内容过大(上限 1MiB),请分批导入')
    } else {
      ElMessage.error('导入失败:内容中没有可识别的节点')
    }
  } finally {
    importing.value = false
  }
}
</script>

<style scoped>
.guide {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
  line-height: 1.6;
  margin-bottom: var(--ph-space-3);
}
.paste-box {
  margin-bottom: var(--ph-space-4);
}
.usage-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.import-result {
  margin-top: var(--ph-space-4);
}
.failure-table {
  margin-top: var(--ph-space-2);
}
</style>
