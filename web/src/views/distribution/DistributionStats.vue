<template>
  <div>
    <div
      style="
        margin-bottom: 16px;
        display: flex;
        justify-content: space-between;
        align-items: center;
      "
    >
      <el-radio-group v-model="timeRange" @change="handleTimeRangeChange">
        <el-radio-button label="today">今天</el-radio-button>
        <el-radio-button label="7days">最近7天</el-radio-button>
        <el-radio-button label="30days">最近30天</el-radio-button>
        <el-radio-button label="custom">自定义</el-radio-button>
      </el-radio-group>

      <el-date-picker
        v-if="timeRange === 'custom'"
        v-model="customRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        style="margin-left: 12px"
        @change="loadStats"
      />
    </div>

    <el-row :gutter="16" style="margin-bottom: 16px">
      <el-col :xs="24" :sm="8">
        <el-card shadow="hover">
          <el-statistic title="总上传" :value="formatBytes(summary.upload_bytes)" />
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card shadow="hover">
          <el-statistic title="总下载" :value="formatBytes(summary.download_bytes)" />
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card shadow="hover">
          <el-statistic title="总连接数" :value="summary.connections" />
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="hover" style="margin-bottom: 16px">
      <template #header>流量趋势</template>
      <div ref="chartRef" v-loading="loading" style="height: 400px"></div>
    </el-card>

    <el-card shadow="hover">
      <template #header>路径流量明细</template>
      <el-table v-loading="loading" :data="pathStats" border>
        <el-table-column prop="path_name" label="路径名称" min-width="120" />
        <el-table-column label="上传" min-width="120">
          <template #default="{ row }">
            {{ formatBytes(row.upload_bytes) }}
            <el-text type="info" size="small" style="margin-left: 8px">
              {{ ((row.upload_bytes / summary.upload_bytes) * 100).toFixed(1) }}%
            </el-text>
          </template>
        </el-table-column>
        <el-table-column label="下载" min-width="120">
          <template #default="{ row }">
            {{ formatBytes(row.download_bytes) }}
            <el-text type="info" size="small" style="margin-left: 8px">
              {{ ((row.download_bytes / summary.download_bytes) * 100).toFixed(1) }}%
            </el-text>
          </template>
        </el-table-column>
        <el-table-column label="连接数" width="100">
          <template #default="{ row }">
            {{ row.connections }}
            <el-text type="info" size="small" style="margin-left: 8px">
              {{ ((row.connections / summary.connections) * 100).toFixed(1) }}%
            </el-text>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import type { EChartsOption } from 'echarts'
import { getDistributionStats, type DistributionStat } from '@/api/distribution'

const timeRange = ref<'today' | '7days' | '30days' | 'custom'>('today')
const customRange = ref<[Date, Date]>([new Date(), new Date()])
const loading = ref(false)
const stats = ref<DistributionStat[]>([])
const chartRef = ref<HTMLElement>()
let chartInstance: echarts.ECharts | null = null

const summary = computed(() => {
  const total = {
    upload_bytes: 0,
    download_bytes: 0,
    connections: 0
  }

  stats.value.forEach((stat) => {
    total.upload_bytes += stat.upload_bytes
    total.download_bytes += stat.download_bytes
    total.connections += stat.connections
  })

  return total
})

const pathStats = computed(() => {
  const pathMap = new Map<number, DistributionStat>()

  stats.value.forEach((stat) => {
    if (!pathMap.has(stat.path_id)) {
      pathMap.set(stat.path_id, {
        path_id: stat.path_id,
        path_name: stat.path_name,
        upload_bytes: 0,
        download_bytes: 0,
        connections: 0,
        timestamp: stat.timestamp
      })
    }

    const existing = pathMap.get(stat.path_id)!
    existing.upload_bytes += stat.upload_bytes
    existing.download_bytes += stat.download_bytes
    existing.connections += stat.connections
  })

  return Array.from(pathMap.values())
})

const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

const getTimeRangeParams = () => {
  const now = new Date()
  let startTime: Date
  let endTime = now

  switch (timeRange.value) {
    case 'today':
      startTime = new Date(now.getFullYear(), now.getMonth(), now.getDate())
      break
    case '7days':
      startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      break
    case '30days':
      startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)
      break
    case 'custom':
      startTime = customRange.value[0]
      endTime = customRange.value[1]
      break
    default:
      startTime = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  }

  return {
    start_time: startTime.toISOString(),
    end_time: endTime.toISOString()
  }
}

const loadStats = async () => {
  loading.value = true
  try {
    const params = getTimeRangeParams()
    stats.value = await getDistributionStats(params)
    updateChart()
  } catch (error) {
    ElMessage.error('加载统计数据失败')
  } finally {
    loading.value = false
  }
}

const updateChart = () => {
  if (!chartRef.value) return

  if (!chartInstance) {
    chartInstance = echarts.init(chartRef.value)
  }

  // Group stats by timestamp
  const timeMap = new Map<string, { upload: number; download: number }>()

  stats.value.forEach((stat) => {
    const time = new Date(stat.timestamp).toISOString()
    if (!timeMap.has(time)) {
      timeMap.set(time, { upload: 0, download: 0 })
    }
    const existing = timeMap.get(time)!
    existing.upload += stat.upload_bytes
    existing.download += stat.download_bytes
  })

  const sortedTimes = Array.from(timeMap.keys()).sort()
  const uploadData = sortedTimes.map((time) => timeMap.get(time)!.upload / 1024 / 1024) // Convert to MB
  const downloadData = sortedTimes.map((time) => timeMap.get(time)!.download / 1024 / 1024) // Convert to MB
  const timeLabels = sortedTimes.map((time) =>
    new Date(time).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
  )

  const option: EChartsOption = {
    tooltip: {
      trigger: 'axis',
      axisPointer: {
        type: 'cross'
      }
    },
    legend: {
      data: ['上传', '下载']
    },
    grid: {
      left: '3%',
      right: '4%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timeLabels
    },
    yAxis: {
      type: 'value',
      name: '流量 (MB)',
      axisLabel: {
        formatter: '{value} MB'
      }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        smooth: true,
        data: uploadData,
        itemStyle: {
          color: '#409EFF'
        }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        itemStyle: {
          color: '#67C23A'
        }
      }
    ]
  }

  chartInstance.setOption(option)
}

const handleTimeRangeChange = () => {
  if (timeRange.value !== 'custom') {
    loadStats()
  }
}

onMounted(() => {
  loadStats()
  window.addEventListener('resize', () => {
    chartInstance?.resize()
  })
})

onUnmounted(() => {
  chartInstance?.dispose()
})
</script>
