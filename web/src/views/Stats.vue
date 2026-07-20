<template>
  <div>
    <!-- 全局汇总卡片 -->
    <el-row :gutter="20">
      <el-col :span="8">
        <el-card><el-statistic title="总拉取次数" :value="global.total_pulls" /></el-card>
      </el-col>
      <el-col :span="8">
        <el-card><el-statistic title="独立 IP 数" :value="global.unique_ips" /></el-card>
      </el-col>
      <el-col :span="8">
        <el-card><el-statistic title="活跃订阅 (24h)" :value="global.active_endpoints" /></el-card>
      </el-col>
    </el-row>

    <!-- 趋势图 -->
    <el-card class="section-card">
      <template #header>
        <div class="card-header">
          <span>拉取趋势</span>
          <el-radio-group v-model="trendDays" size="small" @change="loadTrend">
            <el-radio-button :value="7">最近 7 天</el-radio-button>
            <el-radio-button :value="30">最近 30 天</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <v-chart v-if="hasTrend" :option="chartOption" class="trend-chart" autoresize />
      <el-empty v-else description="暂无拉取数据" :image-size="80" />
    </el-card>

    <!-- 按订阅地址查看 IP 明细 -->
    <el-card class="section-card">
      <template #header>
        <div class="card-header">
          <span>订阅地址访问明细</span>
          <el-select v-model="selectedEndpoint" placeholder="选择订阅地址" class="ctl-endpoint">
            <el-option v-for="ep in endpoints" :key="ep.id" :label="ep.alias" :value="ep.id" />
          </el-select>
        </div>
      </template>
      <IPStatsTable v-if="selectedEndpoint" :endpoint-id="selectedEndpoint" />
      <el-empty v-else description="请选择一个订阅地址" :image-size="60" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import client from '@/api/client'
import type { Endpoint } from '@/types'
import IPStatsTable from '@/components/IPStatsTable.vue'

use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

interface TrendPoint {
  date: string
  endpoint_id: number
  alias: string
  count: number
}

const global = ref({ total_pulls: 0, unique_ips: 0, active_endpoints: 0 })
const trend = ref<TrendPoint[]>([])
const trendDays = ref(7)
const endpoints = ref<Endpoint[]>([])
const selectedEndpoint = ref<number | null>(null)

const hasTrend = computed(() => trend.value.length > 0)

// 图表配色取自设计令牌(随亮/暗主题变化),不硬编码色值;多订阅地址按序循环取色
const chartPalette = (): string[] => {
  const s = getComputedStyle(document.documentElement)
  const read = (name: string, fallback: string) => s.getPropertyValue(name).trim() || fallback
  return [
    read('--ph-color-primary', '#4f46e5'),
    read('--ph-success', '#059669'),
    read('--ph-warning', '#d97706'),
    read('--ph-danger', '#dc2626'),
    read('--ph-info', '#475569'),
    read('--ph-indigo-400', '#818cf8')
  ]
}

// 把扁平的 trend 点按订阅地址分线，日期为 x 轴
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

const loadGlobal = async () => {
  global.value = await client.get('/stats/global')
}

const loadTrend = async () => {
  const data = await client.get<unknown, { trend: TrendPoint[] }>(
    `/stats/trend?days=${trendDays.value}`
  )
  trend.value = data.trend || []
}

const loadEndpoints = async () => {
  endpoints.value = await client.get('/endpoints')
  if (endpoints.value.length && !selectedEndpoint.value) {
    selectedEndpoint.value = endpoints.value[0].id
  }
}

onMounted(() => {
  loadGlobal()
  loadTrend()
  loadEndpoints()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.section-card {
  margin-top: var(--ph-space-5);
}
.trend-chart {
  height: 360px;
}
.ctl-endpoint {
  width: 220px;
}
</style>
