<template>
  <!-- 精选节点选择器(issue #80,spec #70):左侧池节点(名称/来源/地区搜索),
       右侧已选列表可移除;空精选 = 不精选 = 全量(后端零回归语义) -->
  <el-dialog
    :model-value="modelValue"
    title="精选节点"
    width="780px"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @open="onOpen"
  >
    <div class="picks-toolbar">
      <el-input v-model="keyword" clearable placeholder="搜索名称 / 来源 / 地区" />
      <span class="cfg-hint">已选 {{ selected.length }} 个;空 = 不精选(全量)</span>
    </div>
    <div v-loading="loading" class="picks-body">
      <div class="picks-col">
        <div class="picks-col-title">节点池</div>
        <el-table :data="filteredPool" size="small" max-height="360">
          <el-table-column label="节点" min-width="170" show-overflow-tooltip>
            <template #default="{ row }">{{ row.display_name || row.name }}</template>
          </el-table-column>
          <el-table-column prop="source" label="来源" width="110" show-overflow-tooltip />
          <el-table-column prop="region" label="地区" width="70" />
          <el-table-column label="操作" width="70">
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                :disabled="selectedSet.has(row.node_key)"
                @click="add(row.node_key)"
                >添加</el-button
              >
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div class="picks-col">
        <div class="picks-col-title">已选({{ selected.length }})</div>
        <el-table :data="selectedRows" size="small" max-height="360">
          <el-table-column label="节点" min-width="170" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.node">{{ row.node.display_name || row.node.name }}</span>
              <!-- 池中已消失的 key:保留在配置里(复活自动恢复,后端语义),标记已失效 -->
              <span v-else>
                <el-tag type="danger" size="small">已失效</el-tag>
                <span class="picks-invalid-key">{{ row.key }}</span>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="地区" width="70">
            <template #default="{ row }">{{ row.node?.region ?? '-' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="70">
            <template #default="{ row }">
              <el-button link type="danger" @click="remove(row.key)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint, Node, NodePage } from '@/types'
import client from '@/api/client'
import { updateEndpointNodePicks } from '@/api/endpoints'
import { parseNodePicks } from './endpoint-nodepicks-utils'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    endpoint: Endpoint | null // null = 新建暂存模式(端点尚未创建,无 id 可 PUT)
    stagedPicks?: string[] // 新建暂存模式下的初始已选
  }>(),
  { stagedPicks: () => [] }
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void // 编辑模式保存成功(PUT 已落库)
  (e: 'confirm', picks: string[]): void // 新建暂存模式确认(由父级随创建落库)
}>()

// 池规模有界(本地管理面,量级为百),一次性取全量,同 useNodePool 的处理
const POOL_PAGE_SIZE = 100000

const pool = ref<Node[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const selected = ref<string[]>([])

const selectedSet = computed(() => new Set(selected.value))
const poolByKey = computed(() => new Map(pool.value.map((n) => [n.node_key, n])))

// 已选行:按 NodeKey 回连池节点取显示名;池中已消失的 key 置 null(界面标已失效)
const selectedRows = computed(() =>
  selected.value.map((key) => ({ key, node: poolByKey.value.get(key) ?? null }))
)

// 池过滤:名称/显示名/来源/地区子串,大小写不敏感
const filteredPool = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return pool.value
  return pool.value.filter((n) =>
    [n.name, n.display_name, n.source, n.region].some((s) => (s || '').toLowerCase().includes(kw))
  )
})

// 打开时初始化:编辑模式回显端点已配精选,新建模式取暂存;池节点仅首次拉取
const onOpen = async () => {
  keyword.value = ''
  selected.value = props.endpoint
    ? parseNodePicks(props.endpoint.node_picks)
    : [...props.stagedPicks]
  if (pool.value.length > 0) return
  loading.value = true
  try {
    const data = await client.get<unknown, NodePage>('/nodes', {
      params: { page: 1, page_size: POOL_PAGE_SIZE }
    })
    pool.value = data.nodes || []
  } finally {
    loading.value = false
  }
}

const add = (key: string) => {
  if (!selectedSet.value.has(key)) selected.value = [...selected.value, key]
}

const remove = (key: string) => {
  selected.value = selected.value.filter((k) => k !== key)
}

// 编辑模式直接 PUT 落库(空数组 = 清空精选 = 恢复全量);
// 新建暂存模式上抛 confirm,由父级在创建成功后补 PUT。
const save = async () => {
  if (props.endpoint == null) {
    emit('confirm', [...selected.value])
    emit('update:modelValue', false)
    return
  }
  saving.value = true
  try {
    await updateEndpointNodePicks(props.endpoint.id, selected.value)
    ElMessage.success('精选节点已保存')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.picks-toolbar {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-3);
}
.picks-toolbar .el-input {
  flex: 1;
}
.picks-body {
  display: flex;
  gap: var(--ph-space-3);
}
.picks-col {
  flex: 1;
  min-width: 0;
}
.picks-col-title {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  margin-bottom: var(--ph-space-2);
}
.picks-invalid-key {
  margin-left: var(--ph-space-1);
  color: var(--ph-text-secondary);
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  white-space: nowrap;
}
</style>
