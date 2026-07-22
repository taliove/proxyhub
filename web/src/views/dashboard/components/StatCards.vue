<template>
  <!-- 统计卡 + 系统状态:合并为单卡次要层级,紧凑呈现(数据项与迁移前一致) -->
  <el-card class="stat-cards" shadow="never">
    <div class="stat-grid">
      <div v-for="card in statCards" :key="card.key" class="stat-item">
        <div class="stat-label">{{ card.label }}</div>
        <div class="stat-value">{{ card.value }}</div>
        <div class="stat-caption">{{ card.caption }}</div>
      </div>
    </div>
    <div class="status-strip">
      <span class="status-item">
        <span class="status-label">最近更新</span>
        <span class="status-value">{{ stats.lastUpdate }}</span>
      </span>
      <span class="status-item">
        <span class="status-label">平均延迟</span>
        <span class="status-value">{{ stats.avgLatency }}<span class="status-unit">ms</span></span>
      </span>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useDashboardStats } from '../composables/useDashboardStats'

const { stats } = useDashboardStats()

// 统计项:标签/数值/次要说明按灰阶分层,数据内容与迁移前零回归
const statCards = computed(() => [
  {
    key: 'totalNodes',
    label: '总节点数',
    value: stats.value.totalNodes,
    caption: `可用 ${stats.value.availableNodes}`
  },
  {
    key: 'availableNodes',
    label: '可用节点',
    value: stats.value.availableNodes,
    caption: '当前订阅可下发'
  },
  {
    key: 'endpoints',
    label: '订阅地址',
    value: stats.value.endpoints,
    caption: '对外分发入口'
  },
  {
    key: 'airports',
    label: '机场数量',
    value: stats.value.airports,
    caption: '已接入上游'
  }
])
</script>

<style scoped>
.stat-cards {
  border-radius: var(--ph-radius-lg);
}
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--ph-space-4);
}
/* 窄屏降为两列,保持行间纵向间距由 grid gap 承担 */
@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
.stat-label {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-bottom: var(--ph-space-1);
}
.stat-value {
  font-size: var(--ph-text-2xl);
  font-weight: 600;
  line-height: 1.2;
  color: var(--ph-text-primary);
}
.stat-caption {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.status-strip {
  display: flex;
  gap: var(--ph-space-6);
  margin-top: var(--ph-space-4);
  padding-top: var(--ph-space-3);
  border-top: 1px solid var(--ph-border-light);
}
.status-item {
  display: inline-flex;
  align-items: baseline;
  gap: var(--ph-space-2);
}
.status-label {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.status-value {
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.status-unit {
  margin-left: 2px;
  font-size: var(--ph-text-xs);
  font-weight: 400;
  color: var(--ph-text-secondary);
}
</style>
