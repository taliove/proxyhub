<template>
  <el-card>
    <div class="history-title">历史（按标注聚合）</div>
    <el-table v-loading="loading" :data="rows" size="small" @row-click="openDetail">
      <el-table-column label="标注" min-width="180">
        <template #default="{ row }">
          <span class="mark-cell">
            <span>{{ labelOf(row.nodeKey) }}</span>
            <el-tag v-if="row.isDirect" size="small" type="success" effect="plain">直连</el-tag>
            <el-tag v-if="row.orphan" size="small" type="info" effect="plain">已失效</el-tag>
          </span>
        </template>
      </el-table-column>
      <el-table-column label="次数" width="64">
        <template #default="{ row }">
          <span class="num">{{ row.count }}</span>
        </template>
      </el-table-column>
      <el-table-column label="最近实测" width="140">
        <template #default="{ row }">
          <span class="muted num">{{ formatDateTime(row.latestAt) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="下行 Mbps" width="100">
        <template #default="{ row }">
          <span class="num">{{ row.downMbps.toFixed(1) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="上行 Mbps" width="100">
        <template #default="{ row }">
          <span class="num">{{ row.upMbps.toFixed(1) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="空闲延迟 ms" width="104">
        <template #default="{ row }">
          <span class="num">{{ row.idleLatencyMs.toFixed(0) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="抖动 ms" width="88">
        <template #default="{ row }">
          <span class="num">{{ row.jitterMs.toFixed(1) }}</span>
        </template>
      </el-table-column>
      <!-- 与直连基线的差值(节点开销 = 经节点 - 直连):下行负值/延迟正值为开销 -->
      <el-table-column label="Δ下行" width="96">
        <template #default="{ row }">
          <span
            v-if="row.deltaDownMbps !== null"
            class="num"
            :class="deltaClass(row.deltaDownMbps, 'speed')"
          >
            {{ signed(row.deltaDownMbps, 1) }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="Δ延迟" width="96">
        <template #default="{ row }">
          <span
            v-if="row.deltaLatencyMs !== null"
            class="num"
            :class="deltaClass(row.deltaLatencyMs, 'latency')"
          >
            {{ signed(row.deltaLatencyMs, 0) }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="Δ上行" width="96">
        <template #default="{ row }">
          <span
            v-if="row.deltaUpMbps !== null"
            class="num"
            :class="deltaClass(row.deltaUpMbps, 'speed')"
          >
            {{ signed(row.deltaUpMbps, 1) }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="Δ抖动" width="96">
        <template #default="{ row }">
          <span
            v-if="row.deltaJitterMs !== null"
            class="num"
            :class="deltaClass(row.deltaJitterMs, 'latency')"
          >
            {{ signed(row.deltaJitterMs, 0) }}
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <template #empty>
        <span class="muted">暂无实测记录，标注链路后点「开始实测」</span>
      </template>
    </el-table>

    <!-- 桶内原始记录:右侧抽屉(设计规范:详情唯一容器),保留删除入口 -->
    <el-drawer v-model="detailVisible" :title="detailTitle" size="480px">
      <el-table :data="detailRecords" size="small">
        <el-table-column label="时间" width="140">
          <template #default="{ row }">
            <span class="num">{{ formatDateTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="下行" width="80">
          <template #default="{ row }">
            <span class="num">{{ row.down_mbps.toFixed(1) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="上行" width="80">
          <template #default="{ row }">
            <span class="num">{{ row.up_mbps.toFixed(1) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="延迟" width="70">
          <template #default="{ row }">
            <span class="num">{{ row.idle_latency_ms.toFixed(0) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="70">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="emit('delete', row.id)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>
  </el-card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SpeedtestResult } from '@/api/speedtest'
import { formatDateTime } from '../utils'
import type { AggregateRow } from '../composables/useSpeedtestHistory'

const props = defineProps<{
  rows: AggregateRow[]
  records: SpeedtestResult[]
  loading: boolean
  labelOf: (key: string) => string
}>()

const emit = defineEmits<{ (e: 'delete', id: number): void }>()

// 差值符号与语义色:速率类高为优,延迟类低为优。
const signed = (v: number, digits: number): string => `${v > 0 ? '+' : ''}${v.toFixed(digits)}`
const deltaClass = (v: number, kind: 'speed' | 'latency'): string => {
  const good = kind === 'speed' ? v > 0 : v < 0
  if (v === 0) return ''
  return good ? 'delta-good' : 'delta-bad'
}

// 桶明细抽屉:按行 nodeKey 过滤原始记录(时间倒序,后端已排序)。
const detailVisible = ref(false)
const detailKey = ref<string | null>(null)
const detailRecords = computed(() =>
  detailKey.value === null ? [] : props.records.filter((r) => r.node_key === detailKey.value)
)
const detailTitle = computed(() =>
  detailKey.value === null ? '' : `实测记录 · ${props.labelOf(detailKey.value)}`
)
const openDetail = (row: AggregateRow) => {
  detailKey.value = row.nodeKey
  detailVisible.value = true
}
</script>

<style scoped>
.history-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-3);
}
.mark-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
.delta-good {
  color: var(--ph-success);
}
.delta-bad {
  color: var(--ph-danger);
}
</style>
