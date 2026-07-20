<template>
  <div class="dashboard">
    <!-- 统计卡片:大数字为主视觉,标签与次要说明按灰阶分层 -->
    <el-row :gutter="16" class="stat-row">
      <el-col v-for="card in statCards" :key="card.key" :xs="12" :sm="6">
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">{{ card.label }}</div>
          <div class="stat-value">{{ card.value }}</div>
          <div class="stat-caption">{{ card.caption }}</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 系统状态:布局收敛,标题层级靠字重/灰度表达 -->
    <el-card class="status-card" shadow="never">
      <template #header>
        <span class="status-title">系统状态</span>
      </template>
      <el-descriptions :column="2" :colon="false">
        <el-descriptions-item label="最近更新">
          <span class="status-value">{{ stats.lastUpdate }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="平均延迟">
          <span class="status-value"
            >{{ stats.avgLatency }}<span class="status-unit">ms</span></span
          >
        </el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import client from '@/api/client'

interface DashboardStats {
  totalNodes: number
  availableNodes: number
  endpoints: number
  airports: number
  lastUpdate: string
  avgLatency: number
}

const stats = ref<DashboardStats>({
  totalNodes: 0,
  availableNodes: 0,
  endpoints: 0,
  airports: 0,
  lastUpdate: '-',
  avgLatency: 0
})

// 统计卡片:主视觉大数字 + 次要说明文字(灰阶分层),不改数据内容只改呈现层级
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

onMounted(async () => {
  const data = await client.get<unknown, DashboardStats>('/dashboard/stats')
  stats.value = data
})
</script>

<style scoped>
.stat-row {
  margin-bottom: var(--ph-space-2);
}
.stat-card {
  border-radius: var(--ph-radius-lg);
  /* 移动端换行时(xs=12 两列)保证行间纵向间距 */
  margin-bottom: var(--ph-space-3);
}
.stat-label {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  margin-bottom: var(--ph-space-2);
}
.stat-value {
  font-size: 32px;
  font-weight: 700;
  line-height: 1.1;
  color: var(--ph-text-primary);
}
.stat-caption {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.status-card {
  border-radius: var(--ph-radius-lg);
}
.status-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.status-value {
  font-size: var(--ph-text-md);
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
