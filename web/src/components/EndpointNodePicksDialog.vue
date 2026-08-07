<template>
  <!-- 精选节点选择器(issue #80 初版;#86 翻页/页签/全选/别名):
       左侧池节点(页签 + 关键字过滤 + 前端分页),右侧已选列表可移除、可填别名;
       空精选 = 不精选 = 全量(后端零回归语义) -->
  <el-dialog
    :model-value="modelValue"
    title="精选节点"
    width="820px"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
    @open="onOpen"
  >
    <div class="picks-toolbar">
      <el-radio-group v-model="tab" size="small">
        <el-radio-button label="all">全部</el-radio-button>
        <el-radio-button label="self">自建节点</el-radio-button>
        <el-radio-button label="fav">已收藏</el-radio-button>
      </el-radio-group>
      <el-input v-model="keyword" clearable placeholder="搜索名称 / 来源 / 地区" />
    </div>
    <div class="picks-toolbar">
      <el-button size="small" :disabled="filteredPool.length === 0" @click="addAllFiltered">
        全选当前过滤结果({{ filteredPool.length }})
      </el-button>
      <span class="cfg-hint">已选 {{ selected.length }} 个;空 = 不精选(全量)</span>
    </div>
    <div v-loading="loading" class="picks-body">
      <div class="picks-col">
        <div class="picks-col-title">节点池</div>
        <el-table :data="pagedPool" size="small" max-height="360">
          <el-table-column label="节点" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ row.display_name || row.name }}</template>
          </el-table-column>
          <el-table-column prop="source" label="来源" width="100" show-overflow-tooltip />
          <el-table-column prop="region" label="地区" width="64" />
          <el-table-column label="操作" width="64">
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
        <el-pagination
          v-model:current-page="page"
          class="picks-pager"
          layout="total, prev, pager, next"
          :total="filteredPool.length"
          :page-size="POOL_PAGE_SIZE"
          small
        />
      </div>
      <div class="picks-col">
        <div class="picks-col-title">已选({{ selected.length }})</div>
        <el-table :data="selectedRows" size="small" max-height="400">
          <el-table-column label="节点" min-width="130" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.node">{{ row.node.display_name || row.node.name }}</span>
              <!-- 池中已消失的 key:保留在配置里(复活自动恢复,后端语义),标记已失效 -->
              <span v-else>
                <el-tag type="danger" size="small">已失效</el-tag>
                <span class="picks-invalid-key">{{ row.pick.key }}</span>
              </span>
            </template>
          </el-table-column>
          <!-- 精选项别名(issue #85):仅本订阅下发的最终命名层;留空 = 跟随命名链 -->
          <el-table-column label="别名(仅本订阅)" min-width="130">
            <template #default="{ row }">
              <el-input
                :model-value="row.pick.alias ?? ''"
                size="small"
                placeholder="留空跟随命名链"
                maxlength="50"
                @update:model-value="(v: string) => setAlias(row.pick.key, v)"
              />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="64">
            <template #default="{ row }">
              <el-button link type="danger" @click="remove(row.pick.key)">移除</el-button>
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
import {
  parseNodePicks,
  filterPicksPool,
  paginateSlice,
  mergePicks,
  type NodePick,
  type PicksPoolTab
} from './endpoint-nodepicks-utils'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    endpoint: Endpoint | null // null = 新建暂存模式(端点尚未创建,无 id 可 PUT)
    stagedPicks?: NodePick[] // 新建暂存模式下的初始已选
  }>(),
  { stagedPicks: () => [] }
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void // 编辑模式保存成功(PUT 已落库)
  (e: 'confirm', picks: NodePick[]): void // 新建暂存模式确认(由父级随创建落库)
}>()

// 池规模有界(本地管理面,量级为百),一次性取全量后前端过滤分页(issue #86);
// 展示页大小固定 50,节点多时翻页而不是长列表
const POOL_FETCH_SIZE = 100000
const POOL_PAGE_SIZE = 50

const pool = ref<Node[]>([])
const loading = ref(false)
const saving = ref(false)
const tab = ref<PicksPoolTab>('all')
const keyword = ref('')
const page = ref(1)
const selected = ref<NodePick[]>([])

const selectedSet = computed(() => new Set(selected.value.map((p) => p.key)))
const poolByKey = computed(() => new Map(pool.value.map((n) => [n.node_key, n])))

// 已选行:按 NodeKey 回连池节点取显示名;池中已消失的 key 置 null(界面标已失效)
const selectedRows = computed(() =>
  selected.value.map((pick) => ({ pick, node: poolByKey.value.get(pick.key) ?? null }))
)

// 页签 + 关键字过滤(跨全量池),再分页切片;过滤条件变化时收敛页码(越界页由 paginateSlice 兜底)
const filteredPool = computed(() => filterPicksPool(pool.value, tab.value, keyword.value))
const pagedPool = computed(() => paginateSlice(filteredPool.value, page.value, POOL_PAGE_SIZE))

// 打开时初始化:编辑模式回显端点已配精选(双格式兼容),新建模式取暂存;池节点仅首次拉取
const onOpen = async () => {
  tab.value = 'all'
  keyword.value = ''
  page.value = 1
  selected.value = props.endpoint
    ? parseNodePicks(props.endpoint.node_picks)
    : props.stagedPicks.map((p) => ({ ...p }))
  if (pool.value.length > 0) return
  loading.value = true
  try {
    const data = await client.get<unknown, NodePage>('/nodes', {
      params: { page: 1, page_size: POOL_FETCH_SIZE }
    })
    pool.value = data.nodes || []
  } finally {
    loading.value = false
  }
}

const add = (key: string) => {
  if (!selectedSet.value.has(key)) selected.value = [...selected.value, { key }]
}

// 全选当前过滤结果(issue #86):页签 + 关键字过滤出的节点批量并入,按 key 去重
const addAllFiltered = () => {
  selected.value = mergePicks(
    selected.value,
    filteredPool.value.map((n) => n.node_key)
  )
}

const remove = (key: string) => {
  selected.value = selected.value.filter((p) => p.key !== key)
}

// 别名就地更新(替换数组项保持响应式);trim 在后端写边界归一,这里保留原始输入
const setAlias = (key: string, alias: string) => {
  selected.value = selected.value.map((p) => (p.key === key ? { ...p, alias } : p))
}

// 落库前归一:alias trim 后为空则视为无别名(不写空串字段,与后端 omitempty 对齐)
const normalizedPicks = (): NodePick[] =>
  selected.value.map((p) => {
    const alias = (p.alias ?? '').trim()
    return alias ? { key: p.key, alias } : { key: p.key }
  })

// 编辑模式直接 PUT 落库(空数组 = 清空精选 = 恢复全量);
// 新建暂存模式上抛 confirm,由父级在创建成功后补 PUT。
const save = async () => {
  if (props.endpoint == null) {
    emit('confirm', normalizedPicks())
    emit('update:modelValue', false)
    return
  }
  saving.value = true
  try {
    await updateEndpointNodePicks(props.endpoint.id, normalizedPicks())
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
.picks-pager {
  margin-top: var(--ph-space-2);
  justify-content: flex-end;
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
