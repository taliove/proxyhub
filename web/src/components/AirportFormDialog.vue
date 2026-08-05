<template>
  <el-dialog v-model="visible" :title="airport ? '编辑机场' : '添加机场'" width="600px">
    <el-form :model="form" label-width="100px">
      <el-form-item label="名称">
        <el-input v-model="form.name" placeholder="例如：机场A" @input="onNameInput" />
      </el-form-item>
      <!-- 来源二选一(创建时选择,创建后不可变):拉取型填订阅 URL;
           手动机场用于机场禁止外网拉取其订阅的场景,创建后粘贴导入节点。 -->
      <el-form-item v-if="!airport" label="来源">
        <el-radio-group v-model="form.sourceType">
          <el-radio value="url">订阅 URL 拉取</el-radio>
          <el-radio value="manual">手动粘贴导入</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="form.sourceType !== 'manual'" label="订阅 URL">
        <el-input v-model="form.url" placeholder="https://..." />
      </el-form-item>
      <el-form-item label="简称">
        <el-input
          v-model="form.abbr"
          placeholder="留空则自动生成(如 极速机场 → JS)"
          maxlength="16"
          @input="abbrDirty = true"
        />
        <div class="form-hint">
          用于节点名称标准化(如 🇭🇰 香港 JS-01),留空按拼音/字母首字母自动生成
        </div>
      </el-form-item>
      <!-- 手动机场:用量信息手填(全部可选),创建成功后由父级打开粘贴导入对话框 -->
      <template v-if="form.sourceType === 'manual'">
        <AirportUsageFields v-model="usageForm" />
        <el-form-item v-if="!airport">
          <div class="form-hint">
            保存后将打开粘贴导入对话框,粘贴机场面板导出的订阅内容完成入池。
          </div>
        </el-form-item>
      </template>
      <!-- 拉取型机场:官网手填(可选;用量由订阅响应头自动捕获,不开放手填) -->
      <AirportUsageFields v-else v-model="usageForm" web-page-only />
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="submitForm">{{ airport ? '保存' : '添加' }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import client from '@/api/client'
import { useDebouncedSuggest } from '@/composables/useDebouncedSuggest'
import AirportUsageFields from '@/components/AirportUsageFields.vue'
import {
  usageFormFromAirport,
  usageFormToPayload,
  usageFormToPayloadOrZero,
  type UsageFormValue
} from '@/views/airport-utils'

// 机场添加/编辑对话框(ticket 0036 行内收敛后从 Airports.vue 抽出,文件行数门禁)。
// 编辑时来源类型不可变;手动机场隐藏 URL、展示用量手填字段;
// 拉取型机场展示官网手填(用量由订阅响应头自动捕获,不开放)。
const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  // 待编辑机场;null = 添加模式
  airport: Airport | null
}>()

const emit = defineEmits<{
  // 保存成功:airport 为后端返回的最新行,isNew 区分添加/编辑
  (e: 'saved', airport: Airport, isNew: boolean): void
}>()

const emptyUsageForm = (): UsageFormValue => ({
  remainingGb: null,
  totalGb: null,
  expireDate: '',
  webPageUrl: ''
})

const form = ref({ name: '', url: '', abbr: '', sourceType: 'url' as 'url' | 'manual' })
const usageForm = ref<UsageFormValue>(emptyUsageForm())

// 打开时按模式初始化表单(编辑预填,添加清空)
watch(
  () => [visible.value, props.airport?.id] as const,
  ([open]) => {
    if (!open) return
    if (props.airport) {
      form.value = {
        name: props.airport.name,
        url: props.airport.url,
        abbr: props.airport.abbr || '',
        sourceType: props.airport.source_type === 'manual' ? 'manual' : 'url'
      }
      usageForm.value =
        props.airport.source_type === 'manual'
          ? usageFormFromAirport(props.airport)
          : { ...emptyUsageForm(), webPageUrl: props.airport.web_page_url ?? '' }
      // 已有简称视为用户自定义,避免被自动建议覆盖
      abbrDirty.value = !!props.airport.abbr
    } else {
      form.value = { name: '', url: '', abbr: '', sourceType: 'url' }
      usageForm.value = emptyUsageForm()
      resetAbbrSuggest()
    }
  },
  { immediate: true }
)

// 名称输入防抖推导简称(后端单一事实源)
const abbrRef = computed({
  get: () => form.value.abbr,
  set: (val) => {
    form.value.abbr = val
  }
})

const {
  isDirty: abbrDirty,
  onInput: onNameInput,
  reset: resetAbbrSuggest
} = useDebouncedSuggest(abbrRef, async () => {
  const name = form.value.name.trim()
  if (!name) {
    form.value.abbr = ''
    return null
  }
  const res = await client.get<unknown, { abbr: string }>('/airports/abbr-suggest', {
    params: { name }
  })
  return res.abbr || ''
})

const submitForm = async () => {
  const isManual = form.value.sourceType === 'manual'
  // 编辑模式全空也发零值(显式清空必须到达后端);创建模式全空省略(新行本就无用量)
  // 拉取型机场只发官网:编辑始终发(空串 = 显式清空),创建非空才发;
  // 用量三项由订阅响应头自动捕获,绝不随表单提交(后端也只接 web_page_url)。
  const usagePayload = isManual
    ? props.airport
      ? usageFormToPayloadOrZero(usageForm.value)
      : (usageFormToPayload(usageForm.value) ?? {})
    : props.airport || usageForm.value.webPageUrl.trim()
      ? { web_page_url: usageForm.value.webPageUrl.trim() }
      : {}
  if (props.airport) {
    const updated = await client.put<unknown, Airport>(`/airports/${props.airport.id}`, {
      name: form.value.name,
      url: form.value.url,
      abbr: form.value.abbr,
      ...usagePayload
    })
    ElMessage.success('更新成功')
    visible.value = false
    emit('saved', updated, false)
  } else {
    const created = await client.post<unknown, Airport>('/airports', {
      name: form.value.name,
      url: isManual ? '' : form.value.url,
      abbr: form.value.abbr,
      source_type: form.value.sourceType,
      ...usagePayload
    })
    ElMessage.success('添加成功')
    visible.value = false
    emit('saved', created, true)
  }
}
</script>

<style scoped>
.form-hint {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
</style>
