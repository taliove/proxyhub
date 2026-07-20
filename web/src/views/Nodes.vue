<template>
  <el-card>
    <template #header>
      <div class="header">
        <span>节点管理</span>
        <span class="muted" v-if="activeTab === 'airport' && lastUpdate">最后更新：{{ formatTime(lastUpdate) }}</span>
      </div>
    </template>

    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <el-tab-pane label="机场节点" name="airport">
    <!-- 检测进度卡片 -->
    <el-alert
      v-if="detection.running"
      type="info"
      :closable="false"
      style="margin-bottom: 12px"
    >
      <template #title>
        <div style="display: flex; align-items: center; justify-content: space-between">
          <span>正在检测节点解锁状态...</span>
          <el-button size="small" @click="cancelDetection">取消</el-button>
        </div>
      </template>
    </el-alert>

    <!-- 筛选区 -->
    <div class="filter-bar">
      <el-select v-model="filters.region" placeholder="地区" clearable filterable style="width: 140px">
        <el-option v-for="r in regions" :key="r.Code" :label="`${r.Name} (${r.Code})`" :value="r.Code" />
      </el-select>
      <el-select v-model="filters.type" placeholder="类型" clearable style="width: 120px">
        <el-option v-for="t in NODE_TYPES" :key="t" :label="t" :value="t" />
      </el-select>
      <el-select v-model="filters.available" placeholder="可用状态" clearable style="width: 120px">
        <el-option label="可用" value="true" />
        <el-option label="不可用" value="false" />
      </el-select>
      <el-select v-model="filters.blocked" placeholder="屏蔽状态" clearable style="width: 120px">
        <el-option label="已屏蔽" value="true" />
        <el-option label="未屏蔽" value="false" />
      </el-select>
      <el-select v-model="filters.stale" placeholder="上下架" clearable style="width: 120px">
        <el-option label="已下架" value="true" />
        <el-option label="在架" value="false" />
      </el-select>
      <el-input
        v-model="filters.source"
        placeholder="搜索机场"
        clearable
        style="width: 160px"
        @keyup.enter="applyFilters"
      />
      <el-button @click="resetFilters">重置</el-button>
    </div>

    <!-- 批量操作区 -->
    <div class="batch-bar">
      <span class="muted">已选 {{ selectableSelection.length }} 项</span>
      <el-button
        type="danger"
        size="small"
        :disabled="selectableSelection.length === 0"
        @click="blockSelected"
      >屏蔽选中</el-button>
      <el-button
        type="warning"
        size="small"
        :disabled="selectableSelection.length === 0"
        @click="unblockSelected"
      >取消屏蔽</el-button>

      <el-divider direction="vertical" />

      <!-- 按当前筛选机场批量屏蔽:作用于符合筛选条件的整机场,下次生成订阅生效 -->
      <el-select
        v-model="bulkSource"
        placeholder="按机场屏蔽"
        clearable
        filterable
        size="small"
        style="width: 180px"
      >
        <el-option v-for="src in airportSources" :key="src" :label="src" :value="src" />
      </el-select>
      <el-button type="danger" size="small" :disabled="!bulkSource" @click="blockBySource">屏蔽该机场</el-button>
      <el-button type="warning" size="small" :disabled="!bulkSource" @click="unblockBySource">取消该机场</el-button>

      <el-divider direction="vertical" />

      <!-- 解锁检测按钮 -->
      <el-button type="primary" size="small" :disabled="detection.running" @click="detectAll">
        检测全部
      </el-button>
      <el-button type="primary" size="small" :disabled="detection.running" @click="detectFiltered">
        检测筛选结果
      </el-button>
      <el-button
        type="primary"
        size="small"
        :disabled="detection.running || selectableSelection.length === 0"
        @click="detectSelected"
      >
        检测选中
      </el-button>

      <el-divider direction="vertical" />

      <el-button type="danger" size="small" :disabled="detection.running" @click="openCleanup">
        清理失败节点
      </el-button>
    </div>

    <!-- 表格区 -->
    <el-table
      ref="tableRef"
      :data="nodes"
      v-loading="loading"
      row-key="node_key"
      :row-class-name="rowClassName"
      @selection-change="onSelectionChange"
      @sort-change="onSortChange"
    >
      <!-- 展开行:节点详情 + 检测结果清单 -->
      <el-table-column type="expand">
        <template #default="{ row }">
          <!-- 节点完整信息 -->
          <el-descriptions :column="3" border size="small" class="node-info">
            <el-descriptions-item label="服务器">{{ row.server }}</el-descriptions-item>
            <el-descriptions-item label="端口">{{ row.port }}</el-descriptions-item>
            <el-descriptions-item label="协议">{{ row.type }}</el-descriptions-item>
            <el-descriptions-item label="传输">{{ row.network || 'tcp' }}</el-descriptions-item>
            <el-descriptions-item label="TLS">{{ row.tls ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="SNI">{{ row.sni || '—' }}</el-descriptions-item>
            <el-descriptions-item label="地区">{{ row.region || '—' }}</el-descriptions-item>
            <el-descriptions-item label="来源">{{ row.source }}</el-descriptions-item>
            <el-descriptions-item label="延迟">{{ row.latency }}ms</el-descriptions-item>
          </el-descriptions>

          <div class="detect-detail" v-if="row.unlock_results && Object.keys(row.unlock_results).length > 0">
            <el-table :data="unlockRows(row)" size="small" border style="width: 100%; max-width: 640px">
              <el-table-column prop="target" label="检测目标" width="140" />
              <el-table-column label="状态" width="90">
                <template #default="{ row: r }">
                  <el-tag :type="r.available ? 'success' : 'danger'" size="small">
                    {{ r.available ? '通过' : '失败' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="延迟" width="90">
                <template #default="{ row: r }">
                  <span v-if="r.available">{{ r.latency }}ms</span>
                  <span v-else class="muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="失败原因" min-width="240">
                <template #default="{ row: r }">
                  <span v-if="r.available" class="muted">—</span>
                  <span v-else class="error-text">{{ r.error || '不可用' }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <div v-else class="muted" style="padding: 8px 16px">该节点暂无检测记录,点上方「检测」按钮运行检测。</div>

          <!-- 带宽测试结果块 -->
          <div class="bw-detail" v-if="row.bandwidth_down_mbps || row.bandwidth_up_mbps">
            <span class="bw-label">带宽测试:</span>
            <el-tag size="small" type="success">下行 {{ (row.bandwidth_down_mbps || 0).toFixed(1) }} Mbps</el-tag>
            <el-tag size="small" type="success">上行 {{ (row.bandwidth_up_mbps || 0).toFixed(1) }} Mbps</el-tag>
          </div>
        </template>
      </el-table-column>
      <el-table-column type="selection" width="48" :selectable="isSelectable" />
      <el-table-column prop="name" label="原始名称" min-width="160" show-overflow-tooltip sortable="custom" />
      <el-table-column prop="display_name" label="标准名称" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.display_name">{{ row.display_name }}</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="type" label="类型" width="90" />
      <el-table-column prop="region" label="地区" width="90" sortable="custom" />
      <el-table-column prop="latency" label="延迟" width="100" sortable="custom">
        <template #default="{ row }">{{ row.latency }}ms</template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.stale" type="info" size="small" title="机场订阅中已消失,不再下发">已下架</el-tag>
          <el-tag v-else :type="row.available ? 'success' : 'danger'" size="small">
            {{ row.available ? '可用' : '不可用' }}
          </el-tag>
        </template>
      </el-table-column>
      <!-- 带宽列:展示最近一次带宽测试结果 -->
      <el-table-column label="带宽" width="130">
        <template #default="{ row }">
          <span v-if="row.bandwidth_down_mbps || row.bandwidth_up_mbps" class="bw-text">
            ↓{{ (row.bandwidth_down_mbps || 0).toFixed(1) }} / ↑{{ (row.bandwidth_up_mbps || 0).toFixed(1) }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 新增:解锁状态列 -->
      <el-table-column label="解锁" width="100">
        <template #default="{ row }">
          <el-popover
            v-if="row.unlock_results && Object.keys(row.unlock_results).length > 0"
            placement="left"
            width="300"
            trigger="hover"
          >
            <template #reference>
              <el-tag size="small" type="info" style="cursor: pointer">
                {{ unlockSummary(row) }}
              </el-tag>
            </template>
            <div class="unlock-detail">
              <div v-for="(result, target) in row.unlock_results" :key="target" class="unlock-item">
                <div class="unlock-target">
                  <strong>{{ target }}</strong>
                  <el-tag :type="result.available ? 'success' : 'danger'" size="small">
                    {{ result.available ? '✓' : '✗' }}
                  </el-tag>
                </div>
                <div class="unlock-info">
                  <span v-if="result.available" class="muted">{{ result.latency }}ms</span>
                  <span v-else class="error-text">{{ result.error || '不可用' }}</span>
                </div>
              </div>
            </div>
          </el-popover>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="source" label="来源" min-width="120" show-overflow-tooltip sortable="custom" />
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <template v-if="isSelfHosted(row)">
            <el-tag type="info" size="small" style="margin-right: 6px">自建</el-tag>
            <el-button link type="primary" @click="editSelfNode">编辑</el-button>
          </template>
          <template v-else>
            <el-button link type="primary" @click="openOverride(row)">编辑</el-button>
            <el-tag v-if="row.blocked" type="warning" size="small" style="margin-right: 6px">已屏蔽</el-tag>
            <el-button v-if="row.blocked" link type="warning" @click="unblockNode(row)">取消屏蔽</el-button>
            <el-button v-else link type="danger" @click="blockNode(row)">屏蔽</el-button>
          </template>
        </template>
      </el-table-column>
      <el-table-column label="测试" width="200" fixed="right">
        <template #default="{ row }">
          <el-button-group size="small">
            <el-button :disabled="testing" @click="runTest(row, 'quick')">快测</el-button>
            <el-button :disabled="testing" @click="runTest(row, 'real')">真实</el-button>
            <el-button type="success" :disabled="testing" @click="runTest(row, 'bandwidth')">带宽</el-button>
          </el-button-group>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页区 -->
    <div class="pager">
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="load"
        @size-change="onPageSizeChange"
      />
    </div>

    <!-- 机场节点覆盖层编辑弹窗 -->
    <el-dialog v-model="override.visible" title="编辑节点覆盖" width="420px">
      <el-form label-width="90px">
        <el-form-item label="原始名称">
          <span class="muted">{{ override.node?.name || '—' }}</span>
        </el-form-item>
        <el-form-item label="展示名称">
          <el-input v-model="override.displayName" placeholder="留空则用原始名称" clearable />
        </el-form-item>
        <el-form-item label="地区">
          <el-select v-model="override.region" placeholder="留空则用识别地区" clearable filterable style="width: 100%">
            <el-option v-for="r in regions" :key="r.Code" :label="`${r.Name} (${r.Code})`" :value="r.Code" />
          </el-select>
        </el-form-item>
        <el-alert
          type="info"
          :closable="false"
          title="覆盖跨刷新保留,不被下轮机场拉取冲掉。仅可改展示名称/地区。"
        />
      </el-form>
      <template #footer>
        <el-button @click="clearOverride" type="warning" plain>清除覆盖</el-button>
        <el-button @click="override.visible = false">取消</el-button>
        <el-button type="primary" :loading="override.saving" @click="saveOverride">保存</el-button>
      </template>
    </el-dialog>

    <!-- 清理失败节点弹窗 -->
    <el-dialog v-model="cleanup.visible" title="清理失败节点" width="480px">
      <div style="margin-bottom: 12px">
        <el-button
          size="small"
          type="primary"
          :loading="detection.running"
          @click="detectAllThenReload"
        >一键检测全部并刷新</el-button>
        <span class="muted" style="margin-left: 8px">先跑真实检测,再筛出失败节点</span>
      </div>
      <div v-if="cleanup.failedNodes.length === 0" class="muted">
        当前没有不可用节点。可点上方「一键检测全部」后再回到此处。
      </div>
      <template v-else>
        <el-alert type="warning" :closable="false" style="margin-bottom: 12px">
          <template #title>
            将处理 {{ cleanup.failedNodes.length }} 个不可用节点:机场 {{ cleanupAirportCount }} 个、自建 {{ cleanupSelfCount }} 个
          </template>
        </el-alert>
        <el-form label-width="110px">
          <el-form-item label="机场节点">
            <el-tag type="info" size="small">批量屏蔽({{ cleanupAirportCount }} 个)</el-tag>
          </el-form-item>
          <el-form-item label="自建节点处理">
            <el-radio-group v-model="cleanup.selfAction">
              <el-radio value="disable">禁用(可恢复)</el-radio>
              <el-radio value="delete">删除(不可恢复)</el-radio>
            </el-radio-group>
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="cleanup.visible = false">取消</el-button>
        <el-button
          type="danger"
          :loading="cleanup.running"
          :disabled="cleanup.failedNodes.length === 0"
          @click="confirmCleanup"
        >确认处理</el-button>
      </template>
    </el-dialog>

    <!-- 带宽测试弹窗(过程态 + 大数字结果) -->
    <BandwidthTestDialog ref="bwDialog" />
      </el-tab-pane>

      <el-tab-pane label="自建节点" name="self">
        <SelfNodesContent />
      </el-tab-pane>

      <el-tab-pane label="分发节点" name="distribution">
        <DistributionNodesTab />
      </el-tab-pane>
    </el-tabs>
  </el-card>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter, useRoute } from 'vue-router'
import type { Node, NodePage, RegionOption } from '@/types'
import client from '@/api/client'
import { useNodeTest } from '@/composables/useNodeTest'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'
import SelfNodesContent from './SelfNodes.vue'
import DistributionNodesTab from '@/components/DistributionNodesTab.vue'

const router = useRouter()
const route = useRoute()

// Tab 管理
const activeTab = ref<'airport' | 'self' | 'distribution'>('airport')

const handleTabChange = (tab: string) => {
  const validTab = tab === 'self' ? 'self' : tab === 'distribution' ? 'distribution' : 'airport'
  activeTab.value = validTab
  router.push({ query: { tab: validTab === 'airport' ? undefined : validTab } })
}

// 与后端 subscription.SourceSelfHosted 保持一致
const SELF_HOSTED = 'self-hosted'
const NODE_TYPES = ['vmess', 'vless', 'trojan', 'ss', 'hysteria2', 'anytls']

const nodes = ref<Node[]>([])
const regions = ref<{ Code: string; Name: string }[]>([])
const loading = ref(false)
const lastUpdate = ref('')
const selection = ref<Node[]>([])
const bulkSource = ref('')

const filters = reactive({
  region: '',
  type: '',
  available: '',
  blocked: '',
  stale: '',
  source: '',
})

const sort = reactive({ by: 'latency', order: 'asc' })

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

// 检测状态
const detection = reactive({
  running: false,
  pollTimer: null as number | null,
})

const isSelfHosted = (row: Node) => row.source === SELF_HOSTED
const isSelectable = (row: Node) => !isSelfHosted(row)

// stale 节点行置灰(机场订阅中已消失,保留待清理)
const rowClassName = ({ row }: { row: Node }) => (row.stale ? 'stale-row' : '')

const selectableSelection = computed(() => selection.value.filter((n) => !isSelfHosted(n)))

// 节点测试
const { testing, testNode } = useNodeTest()

// runTest 测试单节点。quick/real 走消息提示;bandwidth 走弹窗组件(需过程态 UI)。
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)

const runTest = async (row: Node, mode: 'quick' | 'real' | 'bandwidth') => {
  if (mode === 'bandwidth') {
    bwDialog.value?.open({ node_key: row.node_key }, row.display_name || row.name)
    return
  }
  const res = await testNode({ node_key: row.node_key }, mode)
  if (res) load()
}

// 机场节点覆盖层编辑
const override = reactive({
  visible: false,
  saving: false,
  node: null as Node | null,
  displayName: '',
  region: '',
})

const openOverride = (row: Node) => {
  override.node = row
  override.displayName = row.display_name || ''
  override.region = row.region || ''
  override.visible = true
}

// 自建节点编辑:切换到自建节点 Tab
const editSelfNode = () => {
  router.push({ path: '/nodes', query: { tab: 'self' } })
}

const saveOverride = async () => {
  if (!override.node) return
  override.saving = true
  try {
    await client.put('/nodes/override', {
      node_key: override.node.node_key,
      display_name: override.displayName,
      region: override.region,
    })
    ElMessage.success('已保存,下次生成订阅生效')
    override.visible = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '保存失败')
  } finally {
    override.saving = false
  }
}

const clearOverride = async () => {
  if (!override.node) return
  try {
    await ElMessageBox.confirm('清除覆盖后将恢复机场原始展示信息,确认?', '清除覆盖', {
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await client.delete('/nodes/override', { data: { node_key: override.node.node_key } })
    ElMessage.success('已清除覆盖')
    override.visible = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '清除失败')
  }
}

// 清理失败节点闭环
const cleanup = reactive({
  visible: false,
  running: false,
  failedNodes: [] as Node[],
  selfAction: 'disable' as 'disable' | 'delete',
})

const cleanupAirportCount = computed(
  () => cleanup.failedNodes.filter((n) => !isSelfHosted(n)).length
)
const cleanupSelfCount = computed(
  () => cleanup.failedNodes.filter((n) => isSelfHosted(n)).length
)

const openCleanup = async () => {
  await reloadFailedNodes()
  cleanup.selfAction = 'disable'
  cleanup.visible = true
}

// reloadFailedNodes 拉取全部不可用节点(排除已下架的,已不在订阅无需再处理)
const reloadFailedNodes = async () => {
  try {
    const data = await client.get<any, NodePage>('/nodes', {
      params: { available: 'false', stale: 'false', page_size: 1000, page: 1 },
    })
    cleanup.failedNodes = data.nodes || []
    if (data.total && data.total > 1000) {
      ElMessage.warning(`失败节点超过 1000 个,仅加载前 1000 个`)
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '加载失败节点列表失败')
  }
}

// detectAllThenReload 一键真实检测全部,完成后刷新失败节点列表(spec E 闭环)
const detectAllThenReload = async () => {
  try {
    await client.post('/detection/trigger', { type: 'all' })
    detection.running = true
    ElMessage.info('检测已启动,完成后自动刷新失败列表')
    // 轮询检测状态,完成后重载失败节点
    stopPolling()
    detection.pollTimer = window.setInterval(async () => {
      try {
        const status = await client.get<any, { running: boolean }>('/detection/status')
        if (!status.running) {
          detection.running = false
          stopPolling()
          ElMessage.success('检测完成')
          await reloadFailedNodes()
          load()
        }
      } catch {
        stopPolling()
        detection.running = false
      }
    }, 2000)
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '启动检测失败')
  }
}

const confirmCleanup = async () => {
  const airportKeys = cleanup.failedNodes.filter((n) => !isSelfHosted(n)).map((n) => n.node_key)
  const selfKeys = cleanup.failedNodes.filter((n) => isSelfHosted(n)).map((n) => n.node_key)

  const actionLabel = cleanup.selfAction === 'delete' ? '删除' : '禁用'
  try {
    await ElMessageBox.confirm(
      `将屏蔽 ${airportKeys.length} 个机场节点,${actionLabel} ${selfKeys.length} 个自建节点。此操作${cleanup.selfAction === 'delete' ? '不可恢复' : '可恢复'},确认?`,
      '二次确认',
      { type: 'warning', confirmButtonText: `确认${actionLabel}`, cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  cleanup.running = true
  try {
    let blocked = 0
    let processed = 0
    // 机场节点批量屏蔽
    if (airportKeys.length > 0) {
      const r = await client.post<any, { blocked: number }>('/nodes/cleanup', {
        node_keys: airportKeys,
        action: 'block',
      })
      blocked = r.blocked || 0
    }
    // 自建节点禁用/删除
    if (selfKeys.length > 0) {
      const r = await client.post<any, { disabled: number; deleted: number }>('/nodes/cleanup', {
        node_keys: selfKeys,
        action: cleanup.selfAction,
      })
      processed = (r.disabled || 0) + (r.deleted || 0)
    }
    ElMessage.success(`已屏蔽 ${blocked} 个机场节点,${actionLabel} ${processed} 个自建节点`)
    cleanup.visible = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '清理失败')
  } finally {
    cleanup.running = false
  }
}

// 机场来源下拉:全量机场名(独立于当前分页),供"按机场屏蔽"用
const airportSources = ref<string[]>([])

const onSelectionChange = (rows: Node[]) => {
  selection.value = rows
}

// activeFilterParams 收集非空筛选条件(load 与 detectFiltered 共用,避免重复)
const activeFilterParams = (): Record<string, string> => {
  const p: Record<string, string> = {}
  if (filters.region) p.region = filters.region
  if (filters.type) p.type = filters.type
  if (filters.available) p.available = filters.available
  if (filters.blocked) p.blocked = filters.blocked
  if (filters.stale) p.stale = filters.stale
  if (filters.source) p.source = filters.source
  return p
}

const load = async () => {
  loading.value = true
  try {
    const params: Record<string, string | number> = {
      page: pagination.page,
      page_size: pagination.pageSize,
      sort_by: sort.by,
      sort_order: sort.order,
      ...activeFilterParams(),
    }

    const data = await client.get<any, NodePage>('/nodes', { params })
    nodes.value = data.nodes || []
    pagination.total = data.total || 0
    lastUpdate.value = data.last_update || ''
  } finally {
    loading.value = false
  }
}

// 筛选变化回到第 1 页重新加载
const applyFilters = () => {
  pagination.page = 1
  load()
}

watch(
  () => [filters.region, filters.type, filters.available, filters.blocked, filters.stale],
  applyFilters
)
watch(() => filters.source, applyFilters)

const resetFilters = () => {
  filters.region = ''
  filters.type = ''
  filters.available = ''
  filters.blocked = ''
  filters.stale = ''
  filters.source = ''
  pagination.page = 1
  load()
}

const onPageSizeChange = () => {
  pagination.page = 1
  load()
}

const onSortChange = ({ prop, order }: { prop: string; order: string | null }) => {
  if (!order) {
    sort.by = 'latency'
    sort.order = 'asc'
  } else {
    sort.by = prop
    sort.order = order === 'ascending' ? 'asc' : 'desc'
  }
  pagination.page = 1
  load()
}

const blockNode = async (row: Node) => {
  await client.post('/nodes/block', { node_key: row.node_key })
  ElMessage.success('已屏蔽,下次生成订阅生效')
  load()
}

const unblockNode = async (row: Node) => {
  await client.post('/nodes/unblock', { node_key: row.node_key })
  ElMessage.success('已取消屏蔽')
  load()
}

const blockSelected = async () => {
  const keys = selectableSelection.value.map((n) => n.node_key)
  const res = await client.post<any, { count: number }>('/nodes/batch-block', { node_keys: keys })
  ElMessage.success(`已屏蔽 ${res.count} 个节点,下次生成订阅生效`)
  load()
}

const unblockSelected = async () => {
  const keys = selectableSelection.value.map((n) => n.node_key)
  const res = await client.post<any, { count: number }>('/nodes/batch-unblock', { node_keys: keys })
  ElMessage.success(`已取消 ${res.count} 个节点屏蔽`)
  load()
}

const blockBySource = async () => {
  const src = bulkSource.value
  try {
    await ElMessageBox.confirm(
      `将屏蔽机场「${src}」当前的全部节点,下次生成订阅生效(刷新后依然保持)。确认?`,
      '按机场批量屏蔽',
      { type: 'warning', confirmButtonText: '确认屏蔽', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  const res = await client.post<any, { count: number }>('/nodes/batch-block', { source: src })
  ElMessage.success(`已屏蔽机场「${src}」的 ${res.count} 个节点`)
  load()
}

const unblockBySource = async () => {
  const src = bulkSource.value
  const res = await client.post<any, { count: number }>('/nodes/batch-unblock', { source: src })
  ElMessage.success(`已取消机场「${src}」的 ${res.count} 个节点屏蔽`)
  load()
}

// 解锁检测相关
const detectAll = async () => {
  await triggerDetection({ type: 'all' })
}

const detectFiltered = async () => {
  await triggerDetection({ type: 'query', query: activeFilterParams() })
}

const detectSelected = async () => {
  const keys = selectableSelection.value.map((n) => n.node_key)
  await triggerDetection({ type: 'selected', node_keys: keys })
}

const triggerDetection = async (scope: any) => {
  try {
    await client.post('/detection/trigger', scope)
    detection.running = true
    ElMessage.info('检测已启动')
    startPolling()
  } catch (err: any) {
    ElMessage.error(err.message || '启动检测失败')
  }
}

const cancelDetection = async () => {
  try {
    await client.post('/detection/cancel', {})
    detection.running = false
    stopPolling()
    ElMessage.info('已取消检测')
    load()
  } catch (err: any) {
    ElMessage.error(err.message || '取消失败')
  }
}

const startPolling = () => {
  stopPolling()
  detection.pollTimer = window.setInterval(async () => {
    try {
      const status = await client.get<any, { running: boolean }>('/detection/status')
      if (!status.running) {
        detection.running = false
        stopPolling()
        ElMessage.success('检测完成')
        load()
      }
    } catch {
      stopPolling()
      detection.running = false
    }
  }, 2000)
}

const stopPolling = () => {
  if (detection.pollTimer) {
    clearInterval(detection.pollTimer)
    detection.pollTimer = null
  }
}

const unlockSummary = (row: Node): string => {
  if (!row.unlock_results) return '—'
  const results = Object.values(row.unlock_results)
  const passed = results.filter((r: any) => r.available).length
  const total = results.length
  return `${passed}/${total}`
}

// unlockRows 把 unlock_results map 转成数组(供展开行子表格渲染)
interface UnlockRow {
  target: string
  available: boolean
  latency: number
  error?: string
}
const unlockRows = (row: Node): UnlockRow[] => {
  if (!row.unlock_results) return []
  return Object.entries(row.unlock_results).map(([target, r]: [string, any]) => ({
    target,
    available: r.available,
    latency: r.latency,
    error: r.error,
  }))
}

const loadRegions = async () => {
  const data = await client.get<any, { regions: RegionOption[] }>('/settings/regions')
  regions.value = (data.regions as any) || []
}

const loadAirportSources = async () => {
  const data = await client.get<any, { name: string }[]>('/airports')
  airportSources.value = (data || []).map((a) => a.name).sort()
}

const formatTime = (t: string) => (t ? new Date(t).toLocaleString('zh-CN') : '')

onMounted(() => {
  // 初始化 Tab 状态（从 URL query 参数读取）
  const tabParam = route.query.tab
  if (tabParam === 'self') {
    activeTab.value = 'self'
  } else if (tabParam === 'distribution') {
    activeTab.value = 'distribution'
  } else if (tabParam && tabParam !== 'airport') {
    // 清理无效的 tab 参数
    router.replace({ query: { ...route.query, tab: undefined } })
  }

  loadRegions()
  loadAirportSources()
  load()
})

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.filter-bar,
.batch-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.filter-bar {
  margin-bottom: 12px;
}
.batch-bar {
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}
.pager {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
.unlock-detail {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.unlock-item {
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding-bottom: 8px;
}
.unlock-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
.unlock-target {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}
.unlock-info {
  font-size: 12px;
}
.error-text {
  color: var(--el-color-danger);
}
.detect-detail {
  padding: 12px 48px;
}
.node-info {
  padding: 12px 48px 0;
}
.bw-detail {
  padding: 8px 48px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.bw-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.bw-text {
  font-size: 12px;
  color: var(--el-color-success);
}
/* stale 节点行置灰 */
:deep(.stale-row) {
  opacity: 0.55;
}
</style>
