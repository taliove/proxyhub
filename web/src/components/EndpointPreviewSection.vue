<template>
  <!-- 下发节点清单段(从 EndpointDetailDrawer 抽取,400 行门禁):
       与 /sub 同一生成链的所见即所得;订阅原文折叠展示,Clash/V2Ray 切换重拉 -->
  <div class="drawer-block">
    <div class="drawer-section-title">下发节点清单</div>
    <div class="preview-toolbar">
      <el-radio-group v-model="format" size="small" @change="loadPreview">
        <el-radio-button label="clash">Clash</el-radio-button>
        <el-radio-button label="v2ray">V2Ray</el-radio-button>
      </el-radio-group>
      <span class="preview-hint">
        共 {{ preview.count }} 个节点（已应用节点范围条件，与终端拉取到的完全一致）
      </span>
    </div>
    <el-table v-loading="previewLoading" :data="preview.nodes" size="small" border max-height="300">
      <el-table-column label="名称" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.display_name || row.name }}</template>
      </el-table-column>
      <el-table-column label="地区" width="72">
        <template #default="{ row }">{{ regionDisplay(row.region) }}</template>
      </el-table-column>
      <el-table-column label="延迟" width="80">
        <template #default="{ row }">
          <span class="num">{{ nodeLatencyText(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="可用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.available ? 'success' : 'info'" size="small">
            {{ row.available ? '可用' : '不可用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="source" label="来源" width="110" show-overflow-tooltip />
      <template #empty>
        <span class="muted">当前节点范围条件下无可下发节点。</span>
      </template>
    </el-table>
    <el-collapse class="raw-collapse">
      <el-collapse-item title="订阅原文" name="raw">
        <el-input
          v-model="preview.content"
          type="textarea"
          :rows="8"
          readonly
          placeholder="(无节点内容)"
        />
      </el-collapse-item>
    </el-collapse>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import client from '@/api/client'
import { regionDisplay } from '@/views/nodes/nodecells'
import type { Endpoint } from '@/types'

// 预览节点(与后端 toNodeViews 输出对齐,只取本段所需字段)
interface PreviewNode {
  name: string
  display_name?: string
  region?: string
  latency?: number
  source?: string
  available: boolean
}

const props = defineProps<{
  endpoint: Endpoint | null
  // 抽屉开关态:打开/换端点时重拉,关闭时清空(防下次闪现旧数据)
  active: boolean
}>()

const format = ref<'clash' | 'v2ray'>('clash')
const preview = ref<{ count: number; content: string; nodes: PreviewNode[] }>({
  count: 0,
  content: '',
  nodes: []
})
const previewLoading = ref(false)

const loadPreview = async () => {
  if (!props.endpoint) return
  previewLoading.value = true
  try {
    preview.value = await client.get(
      `/endpoints/${props.endpoint.id}/preview?format=${format.value}`
    )
  } catch {
    preview.value = { count: 0, content: '', nodes: [] }
  } finally {
    previewLoading.value = false
  }
}

const nodeLatencyText = (n: PreviewNode): string =>
  n.latency && n.latency > 0 ? `${n.latency}ms` : '—'

watch(
  () => [props.active, props.endpoint] as const,
  ([active, endpoint]) => {
    if (active && endpoint) {
      format.value = 'clash'
      loadPreview()
    } else if (!active) {
      preview.value = { count: 0, content: '', nodes: [] }
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.drawer-section-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.preview-toolbar {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-2);
}
.preview-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.raw-collapse {
  margin-top: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
