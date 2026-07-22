<template>
  <!-- 拉取统计:原 /stats 页三块视图(汇总/趋势/单地址 IP 明细)合并为仪表盘单卡,
       数据项与迁移前零回归 -->
  <el-card class="pull-stats" shadow="never">
    <template #header>
      <div class="panel-header">
        <span class="panel-title">拉取统计</span>
        <el-radio-group v-model="trendDays" size="small" @change="loadTrend">
          <el-radio-button :value="7">最近 7 天</el-radio-button>
          <el-radio-button :value="30">最近 30 天</el-radio-button>
        </el-radio-group>
      </div>
    </template>

    <!-- 全局汇总:紧凑行,数值为主视觉 -->
    <div class="summary-strip">
      <div class="summary-item">
        <span class="summary-label">总拉取次数</span>
        <span class="summary-value">{{ global.total_pulls }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">独立 IP 数</span>
        <span class="summary-value">{{ global.unique_ips }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">活跃订阅 (24h)</span>
        <span class="summary-value">{{ global.active_endpoints }}</span>
      </div>
    </div>

    <!-- 趋势图 -->
    <v-chart v-if="hasTrend" :option="chartOption" class="trend-chart" autoresize />
    <el-empty v-else description="暂无拉取数据" :image-size="80" />

    <!-- 按订阅地址查看 IP 明细 -->
    <div class="detail-block">
      <div class="detail-header">
        <span class="detail-title">订阅地址访问明细</span>
        <el-select
          v-model="selectedEndpoint"
          placeholder="选择订阅地址"
          size="small"
          class="ctl-endpoint"
        >
          <el-option v-for="ep in endpoints" :key="ep.id" :label="ep.alias" :value="ep.id" />
        </el-select>
      </div>
      <IPStatsTable v-if="selectedEndpoint" :endpoint-id="selectedEndpoint" />
      <el-empty v-else description="请选择一个订阅地址" :image-size="60" />
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import IPStatsTable from '@/components/IPStatsTable.vue'
import { usePullStats } from '../composables/usePullStats'

use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

const { global, trend, trendDays, endpoints, selectedEndpoint, hasTrend, loadTrend } =
  usePullStats()

// 图表配色只消费语义令牌(随亮/暗主题变化),不硬编码色值;多订阅地址按序循环取色
const chartPalette = (): string[] => {
  const s = getComputedStyle(document.documentElement)
  const read = (name: string, fallback: string) => s.getPropertyValue(name).trim() || fallback
  return [
    read('--ph-color-primary', '#0c8078'),
    read('--ph-success', '#059669'),
    read('--ph-warning', '#d97706'),
    read('--ph-danger', '#dc2626'),
    read('--ph-info', '#45565c'),
    read('--ph-color-primary-hover', '#0faea2')
  ]
}

// 把扁平的 trend 点按订阅地址分线,日期为 x 轴
const chartOption = computed(() => {
  const dates = [...new Set(trend.value.map((p) => p.date))].sort()
  const byAlias = new Map<string, Map<string, number>>()
  for (const p of trend.value) {
    const alias = p.alias || `#${p.endpoint_id}`
    if (!byAlias.has(alias)) byAlias.set(alias, new Map())
    byAlias.get(alias)!.set(p.date, p.count)
  }
  const series = [...byAlias.entries()].map(([alias, dateMap]) => ({
    name: alias,
    type: 'line',
    smooth: true,
    data: dates.map((d) => dateMap.get(d) || 0)
  }))
  return {
    color: chartPalette(),
    tooltip: { trigger: 'axis' },
    legend: { data: [...byAlias.keys()] },
    grid: { left: 40, right: 20, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: dates },
    yAxis: { type: 'value', minInterval: 1 },
    series
  }
})
</script>

<style scoped>
.pull-stats {
  border-radius: var(--ph-radius-lg);
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.panel-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.summary-strip {
  display: flex;
  gap: var(--ph-space-6);
}
.summary-item {
  display: inline-flex;
  align-items: baseline;
  gap: var(--ph-space-2);
}
.summary-label {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.summary-value {
  font-size: var(--ph-text-xl);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.trend-chart {
  height: 360px;
  margin-top: var(--ph-space-4);
}
.detail-block {
  margin-top: var(--ph-space-4);
  padding-top: var(--ph-space-4);
  border-top: 1px solid var(--ph-border-light);
}
.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--ph-space-3);
}
.detail-title {
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-regular);
}
.ctl-endpoint {
  width: 220px;
}
</style>
