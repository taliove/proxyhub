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
      </div>
      <el-input
        v-model="content"
        type="textarea"
        :rows="8"
        placeholder="粘贴机场面板导出的订阅内容(base64 整段或明文多行 ss:// vless:// vmess:// trojan:// anytls://)"
        class="paste-box"
      />

      <div class="usage-title">用量信息(可选)</div>
      <el-form label-width="100px">
        <AirportUsageFields v-model="usage" />
      </el-form>

      <!-- 导入结果:成功 N 条 + 失败 M 行逐行报告(行号+原因) -->
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
      <el-button @click="visible = false">{{ result ? '完成' : '取消' }}</el-button>
      <el-button type="primary" :loading="importing" :disabled="!content.trim()" @click="doImport">
        导入
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import client from '@/api/client'
import AirportUsageFields from '@/components/AirportUsageFields.vue'
import {
  usageFormFromAirport,
  usageFormToPayloadOrZero,
  type UsageFormValue
} from '@/views/airport-utils'

// 手动机场粘贴导入对话框(创建后引导与行内「重新粘贴」共用)。
// 凭证红线:粘贴内容只发本次请求,不落本地存储、不进日志。
const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  airport: Airport | null
}>()

const emit = defineEmits<{
  (e: 'imported', airport: Airport): void
}>()

const content = ref('')
const usage = ref<UsageFormValue>({
  remainingGb: null,
  totalGb: null,
  expireDate: '',
  webPageUrl: ''
})
const importing = ref(false)
const result = ref<{ imported: number; failures: { line: number; reason: string }[] } | null>(null)

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
    // 全空也发零值:显式清空用量必须到达后端(空值 = 清空,省略 = 不动)
    const payload = usageFormToPayloadOrZero(usage.value)
    const resp = await client.post<
      unknown,
      { imported: number; failures: { line: number; reason: string }[] }
    >(`/airports/${props.airport.id}/import`, { content: content.value, ...payload })
    result.value = { imported: resp.imported, failures: resp.failures ?? [] }
    emit('imported', props.airport)
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
