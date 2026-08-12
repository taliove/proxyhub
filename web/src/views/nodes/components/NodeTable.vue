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

      <!-- 收藏(issue #83):行内 star,机场/自建节点均可;展示层标记,不参与订阅下发 -->
      <el-table-column label="收藏" width="64">
        <template #default="{ row }">
          <el-button
            link
            class="fav-btn"
            :title="row.favorite ? '取消收藏' : '收藏'"
            @click.stop="emit('toggle-favorite', row)"
          >
            <el-icon v-if="row.favorite" :size="16" class="fav-on"><StarFilled /></el-icon>
            <el-icon v-else :size="16" class="fav-off"><Star /></el-icon>
          </el-button>
        </template>
      </el-table-column>

      <!-- 名称:标准名优先,原始名副标题;状态(屏蔽/下架/禁用/不可用)降为名称旁小标签 -->
      <el-table-column prop="name" label="名称" min-width="180" sortable="custom">
        <template #default="{ row }">
          <div class="name-cell">
            <span class="name-primary">{{ nameCell(row).primary }}</span>
            <el-tooltip
              v-if="slotKeys.has(row.node_key)"
              content="名称槽位接管：该名称可在名称槽位区转移给其他节点"
              placement="top"
            >
              <el-tag size="small" type="success" effect="plain" class="slot-tag">槽位</el-tag>
            </el-tooltip>
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
      <el-table-column prop="latency" label="延迟" width="100" sortable="custom">
        <template #default="{ row }">
          <StatusDot :tone="healthTone(row)" :label="healthLabel(row)" class="latency-dot" />
          <span class="num">{{ latencyText(row) }}</span>
        </template>
      </el-table-column>
      <!-- 稳定性:最近一次体检的稳定性分 + 语义色;无历史不占位 -->
      <el-table-column label="稳定性" width="96">
        <template #default="{ row }">
          <el-tag
            v-if="badgeFor(row)"
            size="small"
            :type="badgeTagType(badgeFor(row)!.level)"
            :title="badgeFor(row)!.text"
            class="num"
          >
            {{ badgeFor(row)!.score }}
          </el-tag>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <!-- 解锁:通过数摘要,悬浮展开各目标三档语义色;单元格抽 UnlockCell(400 行门禁) -->
      <el-table-column label="解锁" width="88">
        <template #default="{ row }">
          <UnlockCell :row="row" />
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
      <!-- (体检时间列已隐藏:信息密度纪律,用户反馈无意义占位;数据仍在详情抽屉) -->
      <!-- 操作:两来源行布局统一为「详情 | 编辑 | 更多▾」——详情是键盘可达入口(行点击无等价物),
           低频与危险动作收进更多下拉;自建/机场同位同义,消除占位漂移(critique P1)。 -->
      <el-table-column label="操作" width="150">
        <template #default="{ row }">
          <span class="row-ops" @click.stop>
            <el-button link type="primary" @click="emit('view', row)">详情</el-button>
            <el-button v-if="isSelfHosted(row)" link type="primary" @click="emit('edit-self', row)"
              >编辑</el-button
            >
            <el-button v-else link type="primary" @click="emit('edit-override', row)"
              >编辑</el-button
            >
            <el-dropdown trigger="click" @command="(cmd: string) => onRowOp(cmd, row)">
              <el-button link>
                更多<el-icon class="el-icon--right"><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="assign-slot">命名</el-dropdown-item>
                  <el-dropdown-item command="refresh-name">刷新名称</el-dropdown-item>
                  <template v-if="isSelfHosted(row)">
                    <el-dropdown-item command="toggle-self">
                      {{ row.enabled === false ? '启用' : '禁用' }}
                    </el-dropdown-item>
                    <el-dropdown-item command="delete-self" divided>
                      <span class="danger-item">删除</span>
                    </el-dropdown-item>
                  </template>
                  <el-dropdown-item v-else command="toggle-block" divided>
                    <span class="warning-item">{{ row.blocked ? '取消屏蔽' : '屏蔽' }}</span>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </span>
        </template>
      </el-table-column>
      <!-- 检查+分享列合并(所有节点均可检查,支持协议的可分享);单元格抽 NodeTestCell(400 行门禁) -->
      <el-table-column label="检查/分享" width="190" fixed="right">
        <template #default="{ row }">
          <NodeTestCell
            :row="row"
            :detecting="detecting"
            :running-exam-keys="runningExamKeys"
            @test="(r: UnifiedNode, cmd: TestCommand) => emit('test', r, cmd)"
            @copy-link="(r: UnifiedNode) => emit('copy-link', r)"
            @show-qr="(r: UnifiedNode) => emit('show-qr', r)"
          />
        </template>
      </el-table-column>
      <!-- 空态分两种(critique P2):筛选无匹配 vs 池真空;池空给第一步引导(去机场页) -->
      <template #empty>
        <div class="table-empty">
          <p v-if="hasActiveFilter">当前筛选下无匹配节点，试试调整筛选条件。</p>
          <template v-else>
            <p>节点池为空。添加机场并刷新后，节点经健康检查入池、出现在这里。</p>
            <el-button type="primary" @click="emit('go-airports')">去添加机场</el-button>
          </template>
        </div>
      </template>
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
import { Star, StarFilled, WarningFilled, ArrowDown } from '@element-plus/icons-vue'
import StatusDot from '@/components/StatusDot.vue'
import NodeTestCell from './NodeTestCell.vue'
import UnlockCell from './UnlockCell.vue'
import { isSelfHosted } from '../utils'
import {
  latencyText,
  nameCell,
  stateTags,
  healthTone,
  healthLabel,
  tagsDisplay,
  type NodeExamSummary
} from '../nodecells'
import { badgeTagType, type TestCommand } from './node-table-utils'
import type { UnifiedNode } from '../selfmerge'
export interface SortChange {
  prop: string
  order: string | null
}

const props = withDefaults(
  defineProps<{
    nodes: UnifiedNode[]
    loading: boolean
    // detecting:全页共享的解锁检测运行态,行内「出网快速检测」进行中标注/禁用
    detecting: boolean
    page: number
    pageSize: number
    total: number
    // node_key -> 最近一次体检派生摘要;缺省即无历史(不渲染)
    examSummaries?: Record<string, NodeExamSummary | undefined>
    // 进行中的 exam 任务 key 集合(node_key),用于显示"查看进度"按钮
    runningExamKeys?: Set<string>
    // 名称槽位占用的 node_key 集合(issue #98):命中行在名称列加"槽位"标记
    slotKeys?: Set<string>
    // 空态分流:有有效筛选 = 筛选无匹配文案;无 = 池空引导(由装配层用 isActiveCriteria 求值)
    hasActiveFilter?: boolean
  }>(),
  {
    examSummaries: () => ({}),
    runningExamKeys: () => new Set(),
    slotKeys: () => new Set(),
    hasActiveFilter: false
  }
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
  (e: 'toggle-favorite', row: UnifiedNode): void
  (e: 'edit-override', row: UnifiedNode): void
  (e: 'assign-slot', row: UnifiedNode): void
  (e: 'edit-self', row: UnifiedNode): void
  (e: 'toggle-self', row: UnifiedNode): void
  (e: 'delete-self', row: UnifiedNode): void
  (e: 'block', row: UnifiedNode): void
  (e: 'unblock', row: UnifiedNode): void
  (e: 'refresh-name', row: UnifiedNode): void
  (e: 'test', row: UnifiedNode, mode: TestCommand): void
  (e: 'copy-link', row: UnifiedNode): void
  (e: 'show-qr', row: UnifiedNode): void
  // 池空空态的引导动作(装配层负责路由)
  (e: 'go-airports'): void
}>()

// 自建节点也参与批量操作;屏蔽语义仅适用机场节点,处理器内按来源过滤。
const isSelectable = () => true

// stale / 禁用节点行置灰
const rowClassName = ({ row }: { row: UnifiedNode }) =>
  row.stale || row.enabled === false ? 'stale-row' : ''

const onRowClick = (row: UnifiedNode, column: { type?: string } | null) => {
  if (column?.type === 'selection') return
  emit('view', row)
}

// 「更多」下拉命令 → 行事件映射(自建/机场分支模板内已分,这里只做派发)
const onRowOp = (cmd: string, row: UnifiedNode) => {
  if (cmd === 'assign-slot') emit('assign-slot', row)
  else if (cmd === 'refresh-name') emit('refresh-name', row)
  else if (cmd === 'toggle-self') emit('toggle-self', row)
  else if (cmd === 'delete-self') emit('delete-self', row)
  else if (cmd === 'toggle-block') {
    if (row.blocked) emit('unblock', row)
    else emit('block', row)
  }
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
.num {
  font-variant-numeric: tabular-nums;
}
.latency-dot {
  margin-right: var(--ph-space-1);
}
.error-text {
  color: var(--ph-danger);
}
.name-cell {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-1);
}
.name-primary {
  font-weight: 500;
}
.name-secondary {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.slot-tag {
  align-self: flex-start;
}
.state-tags,
.tag-cell {
  display: inline-flex;
  gap: var(--ph-space-1);
  flex-wrap: wrap;
}
.state-tags {
  margin-top: var(--ph-space-1);
}
.egress-cell,
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.egress-code {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.04em;
}
.egress-warn {
  color: var(--ph-warning);
}
.fav-on {
  color: var(--ph-warning);
}
.fav-off {
  color: var(--ph-text-secondary);
}
.fav-btn:hover .fav-off {
  color: var(--ph-warning);
}
.danger-item {
  color: var(--ph-danger);
}
.warning-item {
  color: var(--ph-warning);
}
:deep(.stale-row) {
  opacity: 0.55;
}
.table-empty {
  padding: var(--ph-space-6) 0;
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.table-empty p {
  margin: 0 0 var(--ph-space-3);
}
</style>
