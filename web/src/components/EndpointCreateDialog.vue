<template>
  <!-- 新建订阅地址对话框(从订阅管理页抽出,max-lines 瘦身):
       持有别名/模板/公开名称表单与新建暂存精选链路(issue #80):
       暂存精选经同一选择器(暂存模式)编辑,创建成功后随创建补 PUT 落库 -->
  <el-dialog
    :model-value="modelValue"
    title="新建订阅地址"
    width="500px"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-form :model="form" label-width="90px">
      <el-form-item label="别名">
        <el-input v-model="form.alias" placeholder="例如：老爸的手机" />
      </el-form-item>
      <el-form-item label="配置模板">
        <el-select
          v-model="form.template_name"
          placeholder="跟随默认模板"
          clearable
          class="full-width"
        >
          <el-option v-for="tpl in templates" :key="tpl.name" :label="tpl.name" :value="tpl.name" />
        </el-select>
        <div class="cfg-hint">留空则使用用户默认模板(用户级模板库四级回退链)</div>
      </el-form-item>
      <el-form-item label="公开名称">
        <el-input v-model="form.public_name" placeholder="例如：家里宽带" maxlength="50" />
        <div class="cfg-hint">
          随订阅下发,客户端配置列表显示「ProxyHub · 公开名称」;留空则显示「ProxyHub」。
          别名为私有信息,绝不下发。
        </div>
      </el-form-item>
      <el-form-item label="精选节点">
        <el-button size="small" @click="picksVisible = true">{{ stagedLabel }}</el-button>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" @click="createEndpoint">创建</el-button>
    </template>

    <!-- 新建暂存模式选择器(endpoint=null,confirm 暂存;创建成功后补 PUT) -->
    <EndpointNodePicksDialog
      v-model="picksVisible"
      :endpoint="null"
      :staged-picks="stagedPicks"
      @confirm="onPicksConfirm"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import { updateEndpointNodePicks } from '@/api/endpoints'
import EndpointNodePicksDialog from '@/components/EndpointNodePicksDialog.vue'
import type { NodePick } from '@/components/endpoint-nodepicks-utils'
import { useTemplateList } from '@/composables/useTemplateList'

defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'created'): void // 创建成功(含暂存精选补 PUT),父级刷新列表
}>()

const form = ref({ alias: '', template_name: '', public_name: '' })
const { templates, loadTemplates } = useTemplateList()

// 新建暂存精选(issue #80):端点未创建无 id 可 PUT,先暂存,创建成功补 PUT
const picksVisible = ref(false)
const stagedPicks = ref<NodePick[]>([])
const stagedLabel = computed(() =>
  stagedPicks.value.length ? `精选 ${stagedPicks.value.length} 个节点` : '全量(不精选)'
)
const onPicksConfirm = (picks: NodePick[]) => (stagedPicks.value = picks)

const createEndpoint = async () => {
  const payload: { alias: string; template_name?: string; public_name?: string } = {
    alias: form.value.alias
  }
  if (form.value.template_name) {
    payload.template_name = form.value.template_name
  }
  if (form.value.public_name.trim()) {
    payload.public_name = form.value.public_name.trim()
  }
  const created = await client.post<unknown, Endpoint>('/endpoints', payload)
  // 新建暂存的精选随创建补 PUT(创建接口不收精选字段,issue #80)
  if (stagedPicks.value.length && created?.id) {
    await updateEndpointNodePicks(created.id, stagedPicks.value)
  }
  ElMessage.success('创建成功')
  form.value = { alias: '', template_name: '', public_name: '' }
  stagedPicks.value = []
  emit('update:modelValue', false)
  emit('created')
}

onMounted(loadTemplates)
</script>

<style scoped>
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.full-width {
  width: 100%;
}
</style>
