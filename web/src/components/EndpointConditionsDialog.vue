<template>
  <!-- 节点范围对话框:配置本订阅地址的动态筛选条件(见 internal/subfilter) -->
  <el-dialog
    :model-value="modelValue"
    title="节点范围"
    width="620px"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @open="onOpen"
  >
    <el-form label-width="90px">
      <el-form-item label="机场">
        <el-select
          v-model="form.airports"
          class="cond-full"
          multiple
          clearable
          filterable
          collapse-tags
          collapse-tags-tooltip
          placeholder="不限（留空=全部机场）"
          @change="loadPreview"
        >
          <el-option v-for="a in airportOptions" :key="a" :label="a" :value="a" />
        </el-select>
      </el-form-item>
      <el-form-item label="地区">
        <el-select
          v-model="form.regions"
          class="cond-full"
          multiple
          clearable
          filterable
          collapse-tags
          collapse-tags-tooltip
          placeholder="不限（留空=全部地区）"
          @change="loadPreview"
        >
          <el-option
            v-for="r in regionOptions"
            :key="r.Code"
            :label="`${r.Name} (${r.Code})`"
            :value="r.Code"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="标签">
        <el-select
          v-model="form.tags"
          class="cond-full"
          multiple
          clearable
          filterable
          allow-create
          default-first-option
          collapse-tags
          collapse-tags-tooltip
          placeholder="不限；可自行输入 region:US 等动态标签"
          @change="loadPreview"
        >
          <el-option-group v-for="g in tagGroups" :key="g.label" :label="g.label">
            <el-option v-for="t in g.tags" :key="t.value" :label="t.label" :value="t.value" />
          </el-option-group>
        </el-select>
        <div class="cfg-hint">多个标签为「与」：节点需同时具备所有选中标签。</div>
      </el-form-item>
      <el-form-item label="关键词">
        <el-input
          v-model="form.keyword"
          clearable
          placeholder="节点名包含（大小写不敏感）"
          @input="loadPreview"
        />
      </el-form-item>
      <el-form-item label="命中预览">
        <el-tag :type="preview.count > 0 ? 'success' : 'danger'">
          命中 {{ preview.count }} / 共 {{ preview.total }} 个节点
        </el-tag>
        <span class="cfg-hint cond-hint-inline">按当前节点池实时求值，与订阅拉取一致。</span>
        <!-- 节点明细小表(前 20 个) -->
        <el-table
          v-if="preview.nodes && preview.nodes.length > 0"
          :data="preview.nodes"
          size="small"
          max-height="200"
          class="preview-nodes-table"
        >
          <el-table-column prop="name" label="节点名" width="140" show-overflow-tooltip />
          <el-table-column prop="region" label="地区" width="70" />
          <el-table-column prop="latency" label="延迟（ms）" width="90" />
          <el-table-column prop="source" label="来源" width="100" show-overflow-tooltip />
          <el-table-column label="带宽" width="100">
            <template #default="{ row }">
              <span v-if="row.bandwidth_down_mbps">
                {{ Math.round(row.bandwidth_down_mbps) }}M
              </span>
              <span v-else class="cfg-hint">-</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="preview.count > preview.nodes.length" class="cfg-hint">
          仅展示前 {{ preview.nodes.length }} 个，共命中 {{ preview.count }} 个节点
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint, SubscriptionConditions } from '@/types'
import client from '@/api/client'
import { parseConditions } from '@/utils/conditions'
import { tagLabel } from '@/utils/taglabels'

// 地区选项:后端 /settings/regions 返回大写键(与 web/src/views/nodes 对齐)
interface RegionOption {
  Code: string
  Name: string
}

const props = defineProps<{ modelValue: boolean; endpoint: Endpoint | null }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void
}>()

// 标签选项对齐 internal/nodetag/derive.go 的固定词表;region:<CC> 等动态标签用 allow-create 输入。
// 标签分组展示中文化(选项 label 中文,value 保持英文存储)。
const tagGroups: { label: string; tags: { label: string; value: string }[] }[] = [
  {
    label: '解锁能力',
    tags: [
      { label: tagLabel('nf-full'), value: 'nf-full' },
      { label: tagLabel('nf-originals'), value: 'nf-originals' },
      { label: tagLabel('yt-premium'), value: 'yt-premium' },
      { label: tagLabel('disney-plus'), value: 'disney-plus' },
      { label: tagLabel('openai'), value: 'openai' },
      { label: tagLabel('claude'), value: 'claude' },
      { label: tagLabel('gemini'), value: 'gemini' }
    ]
  },
  {
    label: '稳定性',
    tags: [
      { label: tagLabel('stable-good'), value: 'stable-good' },
      { label: tagLabel('stable-fair'), value: 'stable-fair' },
      { label: tagLabel('stable-poor'), value: 'stable-poor' }
    ]
  },
  {
    label: '出网/质量',
    tags: [
      { label: tagLabel('fast'), value: 'fast' },
      { label: tagLabel('ipv6'), value: 'ipv6' },
      { label: tagLabel('hosting'), value: 'hosting' },
      { label: tagLabel('residential'), value: 'residential' },
      { label: tagLabel('dns-leak'), value: 'dns-leak' }
    ]
  }
]

const form = ref<SubscriptionConditions>({ airports: [], regions: [], tags: [], keyword: '' })

interface PreviewNode {
  name: string
  region: string
  latency: number
  source: string
  bandwidth_down_mbps?: number
  tags?: string[]
}

const preview = ref<{ count: number; total: number; nodes: PreviewNode[] }>({
  count: 0,
  total: 0,
  nodes: []
})
const airportOptions = ref<string[]>([])
const regionOptions = ref<RegionOption[]>([])

// 对话框打开时初始化表单并加载选项/预览。
const onOpen = async () => {
  form.value = parseConditions(props.endpoint?.conditions ?? '')
  preview.value = { count: 0, total: 0, nodes: [] }
  await loadOptions()
  await loadPreview()
}

// 拉取机场/地区选项(仅首次)。
const loadOptions = async () => {
  if (airportOptions.value.length === 0) {
    const data = await client.get<unknown, { name: string }[]>('/airports')
    airportOptions.value = (data || []).map((a) => a.name).sort()
  }
  if (regionOptions.value.length === 0) {
    const data = await client.get<unknown, { regions: RegionOption[] }>('/settings/regions')
    regionOptions.value = data.regions || []
  }
}

// 实时命中预览:把当前(未保存)条件发给后端在节点池上求值,所见即所得。
const loadPreview = async () => {
  try {
    preview.value = await client.post('/endpoints/preview-conditions', form.value)
  } catch {
    // 预览失败不打断编辑,保留上次结果
  }
}

const save = async () => {
  if (props.endpoint == null) return
  await client.put(`/endpoints/${props.endpoint.id}/conditions`, form.value)
  ElMessage.success('节点范围已保存')
  emit('update:modelValue', false)
  emit('saved')
}
</script>

<style scoped>
.cond-full {
  width: 100%;
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.cond-hint-inline {
  margin-left: var(--ph-space-2);
  margin-top: 0;
}
.preview-nodes-table {
  margin-top: var(--ph-space-2);
}
</style>
