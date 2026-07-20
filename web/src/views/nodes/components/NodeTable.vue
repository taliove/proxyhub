<template>
  <div class="node-table">
    <el-table
      v-loading="loading"
      :data="nodes"
      row-key="node_key"
      :row-class-name="rowClassName"
      @selection-change="(rows: Node[]) => emit('selection-change', rows)"
      @sort-change="(payload: SortChange) => emit('sort-change', payload)"
      @row-click="onRowClick"
    >
      <el-table-column type="selection" width="48" :selectable="isSelectable" />
      <el-table-column
        prop="name"
        label="原始名称"
        min-width="160"
        show-overflow-tooltip
        sortable="custom"
      />
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
          <el-tag v-if="row.stale" type="info" size="small" title="机场订阅中已消失,不再下发">
            已下架
          </el-tag>
          <el-tag v-else :type="row.available ? 'success' : 'danger'" size="small">
            {{ row.available ? '可用' : '不可用' }}
          </el-tag>
        </template>
      </el-table-column>
      <!-- 带宽列:展示最近一次带宽测试结果 -->
      <el-table-column label="带宽" width="130">
        <template #default="{ row }">
          <span v-if="row.bandwidth_down_mbps || row.bandwidth_up_mbps" class="bw-text">
            ↓{{ (row.bandwidth_down_mbps || 0).toFixed(1) }} / ↑{{
              (row.bandwidth_up_mbps || 0).toFixed(1)
            }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="解锁" width="100">
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
                    <!-- 通用探测保持既有 ✓/✗;解锁目标用三档语义色 + 文案 -->
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
      <el-table-column
        prop="source"
        label="来源"
        min-width="120"
        show-overflow-tooltip
        sortable="custom"
      />
      <!-- 行内操作 ≤3:编辑 / 屏蔽切换;点击行本身打开详情抽屉 -->
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <span class="row-ops" @click.stop>
            <template v-if="isSelfHosted(row)">
              <el-tag type="info" size="small">自建</el-tag>
              <el-button link type="primary" @click="emit('edit-self')">编辑</el-button>
            </template>
            <template v-else>
              <el-button link type="primary" @click="emit('edit-override', row)">编辑</el-button>
              <el-tag v-if="row.blocked" type="warning" size="small">已屏蔽</el-tag>
              <el-button v-if="row.blocked" link type="warning" @click="emit('unblock', row)">
                取消屏蔽
              </el-button>
              <el-button v-else link type="warning" @click="emit('block', row)">屏蔽</el-button>
            </template>
          </span>
        </template>
      </el-table-column>
      <!-- 测试列独立(所有节点均可测试,包括自建) -->
      <el-table-column label="测试" width="200" fixed="right">
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
                  <el-dropdown-item command="exam" divided>深度体检</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
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
import { ArrowDown } from '@element-plus/icons-vue'
import type { Node } from '@/types'
import { isSelfHosted } from '../utils'
import { isGenericVariant, unlockDisplayRows, unlockSummary } from '../unlock'

export type TestCommand = 'quick' | 'real' | 'bandwidth' | 'exam'
export interface SortChange {
  prop: string
  order: string | null
}

defineProps<{
  nodes: Node[]
  loading: boolean
  testing: boolean
  page: number
  pageSize: number
  total: number
}>()

const emit = defineEmits<{
  (e: 'selection-change', rows: Node[]): void
  (e: 'sort-change', payload: SortChange): void
  (e: 'page-change', page: number): void
  (e: 'size-change', size: number): void
  (e: 'view', row: Node): void
  (e: 'edit-override', row: Node): void
  (e: 'edit-self'): void
  (e: 'block', row: Node): void
  (e: 'unblock', row: Node): void
  (e: 'test', row: Node, mode: TestCommand): void
}>()

const isSelectable = (row: Node) => !isSelfHosted(row)

// stale 节点行置灰(机场订阅中已消失,保留待清理)
const rowClassName = ({ row }: { row: Node }) => (row.stale ? 'stale-row' : '')

const hasUnlock = (row: Node) => !!row.unlock_results && Object.keys(row.unlock_results).length > 0

// 行点击打开详情;选择列的点击(勾选)不算
const onRowClick = (row: Node, column: { type?: string } | null) => {
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
.bw-text {
  font-size: var(--ph-text-xs);
  color: var(--ph-success);
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
/* stale 节点行置灰 */
:deep(.stale-row) {
  opacity: 0.55;
}
</style>
