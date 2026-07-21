<template>
  <div class="node-table">
    <el-table
      v-loading="loading"
      :data="nodes"
      row-key="node_key"
      :row-class-name="rowClassName"
      @selection-change="(rows: UnifiedNode[]) => emit('selection-change', rows)"
      @sort-change="(payload: SortChange) => emit('sort-change', payload)"
      @row-click="onRowClick"
    >
      <el-table-column type="selection" width="48" :selectable="isSelectable" />

      <!-- 名称:标准名优先,原始名副标题;状态(屏蔽/下架/禁用/不可用)降为名称旁小标签 -->
      <el-table-column prop="name" label="名称" min-width="180" sortable="custom">
        <template #default="{ row }">
          <div class="name-cell">
            <span class="name-primary">{{ nameCell(row).primary }}</span>
            <span v-if="nameCell(row).secondary" class="name-secondary">
              {{ nameCell(row).secondary }}
            </span>
            <span v-if="stateTags(row).length" class="state-tags">
              <el-tag
                v-for="s in stateTags(row)"
                :key="s.label"
                :type="s.tone"
                size="small"
                effect="plain"
              >
                {{ s.label }}
              </el-tag>
            </span>
          </div>
        </template>
      </el-table-column>
      <!-- 来源:自建/机场;来源筛选在工具栏 -->
      <el-table-column
        prop="source"
        label="来源"
        min-width="120"
        sortable="custom"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          <el-tag v-if="isSelfHosted(row)" size="small" type="warning" effect="plain">自建</el-tag>
          <span v-else>{{ row.source }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="type" label="类型" width="88" />
      <el-table-column prop="region" label="地区" width="80" sortable="custom">
        <template #default="{ row }">{{ row.region || '—' }}</template>
      </el-table-column>
      <el-table-column prop="latency" label="延迟" width="90" sortable="custom">
        <template #default="{ row }">{{ latencyText(row) }}</template>
      </el-table-column>
      <!-- 稳定性:最近一次体检的稳定性分 + 语义色;无历史不占位 -->
      <el-table-column label="稳定性" width="96">
        <template #default="{ row }">
          <el-tag
            v-if="badgeFor(row)"
            size="small"
            :type="badgeTagType(badgeFor(row)!.level)"
            :title="badgeFor(row)!.text"
          >
            {{ badgeFor(row)!.score }}
          </el-tag>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 解锁:通过数摘要,悬浮展开各目标三档语义色 -->
      <el-table-column label="解锁" width="88">
        <template #default="{ row }">
          <el-popover v-if="hasUnlock(row)" placement="left" width="300" trigger="hover">
            <template #reference>
              <el-tag size="small" type="info" class="unlock-tag">{{ unlockSummary(row) }}</el-tag>
            </template>
            <div class="unlock-detail">
              <div v-for="item in unlockDisplayRows(row)" :key="item.target" class="unlock-item">
                <div class="unlock-target">
                  <strong>{{ item.target }}</strong>
                  <span class="unlock-badges">
                    <el-tag
                      v-if="item.region"
                      size="small"
                      type="info"
                      effect="plain"
                      class="region-badge"
                    >
                      {{ item.region }}
                    </el-tag>
                    <el-tag
                      v-if="isGenericVariant(item.display.variant)"
                      :type="item.display.tagType"
                      size="small"
                    >
                      {{ item.result.available ? '✓' : '✗' }}
                    </el-tag>
                    <el-tag v-else :type="item.display.tagType" size="small">
                      {{ item.display.label }}
                    </el-tag>
                  </span>
                </div>
                <div class="unlock-info">
                  <span v-if="item.result.available" class="muted"
                    >{{ item.result.latency }}ms</span
                  >
                  <span v-else-if="item.display.variant === 'error'" class="muted">
                    {{ item.result.error || '检测失败' }}
                  </span>
                  <span v-else class="error-text">{{ item.result.error || '不可用' }}</span>
                </div>
              </div>
            </div>
          </el-popover>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 出网:体检出口国家码 + 泄露/代理警示 -->
      <el-table-column label="出网" width="96">
        <template #default="{ row }">
          <span v-if="egressFor(row)" class="egress-cell">
            <span v-if="egressFor(row)!.code" class="egress-code">{{ egressFor(row)!.code }}</span>
            <span v-else class="muted">?</span>
            <el-tooltip
              v-if="egressFor(row)!.warn"
              placement="top"
              :content="egressFor(row)!.reasons.join(' / ')"
            >
              <el-icon class="egress-warn"><WarningFilled /></el-icon>
            </el-tooltip>
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 标签:自动标签(票据 21 前无数据,走空态) -->
      <el-table-column label="标签" min-width="120">
        <template #default="{ row }">
          <span v-if="tagsDisplay(row).length" class="tag-cell">
            <el-tag v-for="t in tagsDisplay(row)" :key="t" size="small" effect="plain">{{
              t
            }}</el-tag>
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 体检时间:最近一次体检相对时间 -->
      <el-table-column label="体检时间" width="110">
        <template #default="{ row }">
          <span v-if="badgeFor(row) || summaryFor(row)" class="muted">
            {{ summaryFor(row)?.relative || '—' }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 操作:自建走 编辑/刷新名称/启停/删除;机场走 编辑/刷新名称/屏蔽;点击行打开详情抽屉 -->
      <el-table-column label="操作" width="280">
        <template #default="{ row }">
          <span class="row-ops" @click.stop>
            <template v-if="isSelfHosted(row)">
              <el-button link type="primary" @click="emit('edit-self', row)">编辑</el-button>
              <el-button link @click="emit('refresh-name', row)">刷新名称</el-button>
              <el-button link @click="emit('toggle-self', row)">
                {{ row.enabled === false ? '启用' : '禁用' }}
              </el-button>
              <el-button link type="danger" @click="emit('delete-self', row)">删除</el-button>
            </template>
            <template v-else>
              <el-button link type="primary" @click="emit('edit-override', row)">编辑</el-button>
              <el-button link @click="emit('refresh-name', row)">刷新名称</el-button>
              <el-button v-if="row.blocked" link type="warning" @click="emit('unblock', row)">
                取消屏蔽
              </el-button>
              <el-button v-else link type="warning" @click="emit('block', row)">屏蔽</el-button>
            </template>
          </span>
        </template>
      </el-table-column>
      <!-- 测试+分享列合并(所有节点均可测试,支持协议的可分享) -->
      <el-table-column label="测试/分享" width="190" fixed="right">
        <template #default="{ row }">
          <span class="row-ops" @click.stop>
            <el-dropdown
              size="small"
              trigger="click"
              @command="(mode: TestCommand) => emit('test', row, mode)"
            >
              <el-button link type="primary" :disabled="testing">
                测试
                <el-icon><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="quick">快测</el-dropdown-item>
                  <el-dropdown-item command="real">真实检测</el-dropdown-item>
                  <el-dropdown-item command="bandwidth">带宽测试</el-dropdown-item>
                  <el-dropdown-item command="exam" divided>
                    {{ props.runningExamKeys.has(row.node_key) ? '查看进度' : '深度体检' }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button
              link
              type="primary"
              :icon="DocumentCopy"
              title="复制链接"
              :disabled="!canShare(row)"
              @click="emit('copy-link', row)"
            />
            <el-button
              link
              type="primary"
              :icon="Grid"
              title="二维码"
              :disabled="!canShare(row)"
              @click="emit('show-qr', row)"
            />
          </span>
        </template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="(p: number) => emit('page-change', p)"
        @size-change="(s: number) => emit('size-change', s)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArrowDown, WarningFilled, DocumentCopy, Grid } from '@element-plus/icons-vue'
import { isSelfHosted } from '../utils'
import { isGenericVariant, unlockDisplayRows, unlockSummary } from '../unlock'
import { latencyText, nameCell, stateTags, tagsDisplay, type NodeExamSummary } from '../nodecells'
import { badgeTagType, canShare } from './node-table-utils'
import type { UnifiedNode } from '../selfmerge'

export type TestCommand = 'quick' | 'real' | 'bandwidth' | 'exam'
export interface SortChange {
  prop: string
  order: string | null
}

const props = withDefaults(
  defineProps<{
    nodes: UnifiedNode[]
    loading: boolean
    testing: boolean
    page: number
    pageSize: number
    total: number
    // node_key -> 最近一次体检派生摘要;缺省即无历史(不渲染)
    examSummaries?: Record<string, NodeExamSummary | undefined>
    // 进行中的 exam 任务 key 集合(node_key),用于显示"查看进度"按钮
    runningExamKeys?: Set<string>
  }>(),
  { examSummaries: () => ({}), runningExamKeys: () => new Set() }
)

const summaryFor = (row: UnifiedNode): NodeExamSummary | undefined =>
  props.examSummaries[row.node_key]
const badgeFor = (row: UnifiedNode) => summaryFor(row)?.badge ?? undefined
const egressFor = (row: UnifiedNode) => summaryFor(row)?.egress ?? undefined

const emit = defineEmits<{
  (e: 'selection-change', rows: UnifiedNode[]): void
  (e: 'sort-change', payload: SortChange): void
  (e: 'page-change', page: number): void
  (e: 'size-change', size: number): void
  (e: 'view', row: UnifiedNode): void
  (e: 'edit-override', row: UnifiedNode): void
  (e: 'edit-self', row: UnifiedNode): void
  (e: 'toggle-self', row: UnifiedNode): void
  (e: 'delete-self', row: UnifiedNode): void
  (e: 'block', row: UnifiedNode): void
  (e: 'unblock', row: UnifiedNode): void
  (e: 'refresh-name', row: UnifiedNode): void
  (e: 'test', row: UnifiedNode, mode: TestCommand): void
  (e: 'copy-link', row: UnifiedNode): void
  (e: 'show-qr', row: UnifiedNode): void
}>()

// Self-hosted nodes are now selectable for batch operations.
// Block/unblock operations semantically only apply to airport nodes (self-hosted nodes
// don't participate in blocking); other batch operations (detect, exam, refresh-names)
// apply to all node types uniformly. The batch operation handlers filter by source when needed.
const isSelectable = () => true

// stale / 禁用节点行置灰
const rowClassName = ({ row }: { row: UnifiedNode }) =>
  row.stale || row.enabled === false ? 'stale-row' : ''

const hasUnlock = (row: UnifiedNode) =>
  !!row.unlock_results && Object.keys(row.unlock_results).length > 0

const onRowClick = (row: UnifiedNode, column: { type?: string } | null) => {
  if (column?.type === 'selection') return
  emit('view', row)
}
</script>

<style scoped>
.pager {
  margin-top: var(--ph-space-4);
  display: flex;
  justify-content: flex-end;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.error-text {
  color: var(--ph-danger);
}
.name-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.name-primary {
  font-weight: 500;
}
.name-secondary {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.state-tags {
  display: inline-flex;
  gap: var(--ph-space-1);
  flex-wrap: wrap;
  margin-top: 2px;
}
.tag-cell {
  display: inline-flex;
  gap: var(--ph-space-1);
  flex-wrap: wrap;
}
.egress-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.egress-code {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.5px;
}
.egress-warn {
  color: var(--ph-warning);
}
.unlock-tag {
  cursor: pointer;
}
.unlock-detail {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-3);
}
.unlock-item {
  border-bottom: 1px solid var(--ph-border-light);
  padding-bottom: var(--ph-space-2);
}
.unlock-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
.unlock-target {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-1);
}
.unlock-badges {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.region-badge {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.5px;
}
.unlock-info {
  font-size: var(--ph-text-xs);
}
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
:deep(.stale-row) {
  opacity: 0.55;
}
</style>
