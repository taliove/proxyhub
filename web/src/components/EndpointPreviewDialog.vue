<template>
  <!-- 订阅预览对话框:所见即所得,与真实订阅走同一条节点池→条件过滤→生成链 -->
  <el-dialog
    :model-value="modelValue"
    title="订阅预览"
    width="820px"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="preview-toolbar">
      <el-radio-group v-model="format" @change="loadPreview">
        <el-radio-button label="clash">Clash</el-radio-button>
        <el-radio-button label="v2ray">V2Ray</el-radio-button>
      </el-radio-group>
      <span class="preview-hint">
        共 {{ preview.count }} 个节点(已应用节点范围条件,与终端拉取到的完全一致)
      </span>
    </div>

    <el-table v-loading="loading" :data="preview.nodes" height="260" size="small">
      <el-table-column label="名称" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.display_name || row.name }}</template>
      </el-table-column>
      <el-table-column prop="region" label="地区" width="80" />
      <el-table-column prop="latency" label="延迟(ms)" width="90" />
      <el-table-column label="解锁" width="120">
        <template #default="{ row }">
          <div v-if="row.unlock_results" class="unlock-badges">
            <el-tag
              v-for="(result, target) in row.unlock_results"
              :key="target"
              :type="result.available ? 'success' : 'info'"
              size="small"
              class="unlock-badge"
            >
              {{ target }}
            </el-tag>
          </div>
          <span v-else class="no-unlock">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="source" label="来源" width="100" show-overflow-tooltip />
      <el-table-column label="可用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.available ? 'success' : 'info'" size="small">
            {{ row.available ? '可用' : '不可用' }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>

    <el-input
      v-model="preview.content"
      type="textarea"
      :rows="8"
      readonly
      placeholder="(无节点内容)"
      class="preview-content"
    />

    <template #footer>
      <div class="preview-footer">
        <el-button @click="copySubscriptionLink">复制订阅链接</el-button>
        <el-button :disabled="!preview.content" @click="copyPreview">复制订阅内容</el-button>
        <el-button type="primary" @click="emit('update:modelValue', false)">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'

interface PreviewNode {
  name: string
  display_name?: string
  region?: string
  latency?: number
  source?: string
  available: boolean
  unlock_results?: Record<string, { available: boolean; level?: string }>
  tags?: string[]
}

const props = defineProps<{
  modelValue: boolean
  endpoint: Endpoint | null
  subscriptionUrl: string
}>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()

const loading = ref(false)
const format = ref<'clash' | 'v2ray'>('clash')
const preview = ref<{ count: number; content: string; nodes: PreviewNode[] }>({
  count: 0,
  content: '',
  nodes: []
})

const loadPreview = async () => {
  if (!props.endpoint) return
  loading.value = true
  try {
    preview.value = await client.get(
      `/endpoints/${props.endpoint.id}/preview?format=${format.value}`
    )
  } finally {
    loading.value = false
  }
}

// 打开对话框时重置格式并加载
watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      format.value = 'clash'
      loadPreview()
    }
  }
)

const copyPreview = () => {
  navigator.clipboard.writeText(preview.value.content)
  ElMessage.success('已复制订阅内容')
}

const copySubscriptionLink = () => {
  navigator.clipboard.writeText(props.subscriptionUrl)
  ElMessage.success('已复制订阅链接')
}
</script>

<style scoped>
.preview-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: var(--ph-space-3);
}
.preview-hint {
  margin-left: var(--ph-space-3);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.preview-content {
  margin-top: var(--ph-space-3);
}
.unlock-badges {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ph-space-1);
}
.unlock-badge {
  font-size: var(--ph-text-xs);
}
.no-unlock {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}
.preview-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--ph-space-2);
}
</style>
