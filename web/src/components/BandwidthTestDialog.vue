<template>
  <el-dialog
    v-model="visible"
    :title="`带宽测试 · ${nodeName}`"
    width="540px"
    :close-on-click-modal="!running"
    :close-on-press-escape="!running"
    :show-close="!running"
    @closed="onClosed"
  >
    <!-- 测试中：实时曲线 + 跳动数字 -->
    <div v-if="running" class="bw-testing">
      <v-chart :option="chartOption" autoresize style="height: 240px; margin-bottom: 16px;" />

      <div class="bw-live">
        <div class="bw-live-item">
          <span class="bw-live-label">↓ 下行</span>
          <span class="bw-live-value">{{ liveDown.toFixed(1) }}</span>
          <span class="bw-live-unit">Mbps</span>
        </div>
        <div class="bw-live-item">
          <span class="bw-live-label">↑ 上行</span>
          <span class="bw-live-value">{{ liveUp.toFixed(1) }}</span>
          <span class="bw-live-unit">Mbps</span>
        </div>
      </div>

      <div class="bw-phase">{{ phaseText }}</div>
    </div>

    <!-- 结果：定格曲线 + 大数字卡片 -->
    <div v-else-if="result" class="bw-result">
      <v-chart :option="chartOption" autoresize style="height: 240px; margin-bottom: 16px;" />

      <div class="bw-cards">
        <div class="bw-card">
          <div class="bw-card-label">↓ 下行</div>
          <div class="bw-card-value">{{ fmt(result.down_mbps) }}</div>
          <div class="bw-card-unit">Mbps</div>
        </div>
        <div class="bw-card">
          <div class="bw-card-label">↑ 上行</div>
          <div class="bw-card-value">{{ fmt(result.up_mbps) }}</div>
          <div class="bw-card-unit">Mbps</div>
        </div>
      </div>

      <div class="bw-meta">
        <el-tag v-if="result.available" type="success" size="large">
          ✓ 达标
          <span v-if="result.min_down_mbps || result.min_up_mbps" class="bw-threshold">
            (阈值 {{ fmt(result.min_down_mbps) }}/{{ fmt(result.min_up_mbps) }} Mbps)
          </span>
        </el-tag>
        <el-tag v-else type="danger" size="large">✗ 不达标</el-tag>

        <span v-if="result.elapsed_ms" class="bw-elapsed">
          耗时 {{ (result.elapsed_ms / 1000).toFixed(1) }}s
        </span>
      </div>

      <el-alert
        v-if="!result.available && result.error"
        type="error"
        :closable="false"
        :title="result.error"
        style="margin-top: 12px"
      />
    </div>

    <template #footer>
      <el-button v-if="!running && result" @click="rerun">重新测试</el-button>
      <el-button :disabled="running" type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import type { NodeTestResult, BandwidthSample } from '@/types'

use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

const visible = ref(false)
const running = ref(false)
const result = ref<NodeTestResult | null>(null)
const nodeName = ref('')
const phaseText = ref('正在测下行…')

// 实时跳动数字
const liveDown = ref(0)
const liveUp = ref(0)

// echarts 数据点 {x:秒, y:Mbps}
const downSamples = ref<{x: number; y: number}[]>([])
const upSamples = ref<{x: number; y: number}[]>([])

let currentPayload: { self_node_id?: number; node_key?: string } = {}
let es: EventSource | null = null
let finished = false // 防 onerror 误报

const fmt = (v?: number) => (v ?? 0).toFixed(1)

// echarts option（双曲线:下行/上行）
const chartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    valueFormatter: (v: number) => `${(v ?? 0).toFixed(1)} Mbps`
  },
  legend: { data: ['下行', '上行'], top: 4 },
  grid: { left: 12, right: 24, top: 44, bottom: 28, containLabel: true },
  xAxis: {
    type: 'value',
    name: '秒',
    nameLocation: 'end',
    nameGap: 8,
    min: 0,
    axisLabel: { formatter: '{value}s' }
  },
  yAxis: {
    type: 'value',
    name: 'Mbps',
    nameLocation: 'end',
    nameGap: 12,
    minInterval: 1,
    scale: true
  },
  series: [
    {
      name: '下行',
      type: 'line',
      smooth: true,
      showSymbol: false,
      areaStyle: { opacity: 0.08 },
      // 前置 (0,0):测速从 0 起爬升,曲线从原点开始(否则从首个采样点 ~0.5s 起步)
      data: downSamples.value.length ? [[0, 0], ...downSamples.value.map(p => [p.x, p.y])] : []
    },
    {
      name: '上行',
      type: 'line',
      smooth: true,
      showSymbol: false,
      areaStyle: { opacity: 0.08 },
      data: upSamples.value.length ? [[0, 0], ...upSamples.value.map(p => [p.x, p.y])] : []
    }
  ]
}))

const open = (payload: { self_node_id?: number; node_key?: string }, name: string) => {
  currentPayload = payload
  nodeName.value = name
  result.value = null
  liveDown.value = 0
  liveUp.value = 0
  downSamples.value = []
  upSamples.value = []
  visible.value = true
  runTest()
}

const runTest = () => {
  running.value = true
  result.value = null
  finished = false
  phaseText.value = '正在测下行…'

  const params = new URLSearchParams()
  if (currentPayload.self_node_id) params.set('self_node_id', String(currentPayload.self_node_id))
  if (currentPayload.node_key) params.set('node_key', currentPayload.node_key)

  es = new EventSource(`/api/nodes/test/stream?${params}`, { withCredentials: true })

  es.onmessage = (e) => {
    try {
      const frame = JSON.parse(e.data)
      if (frame.phase === 'download' || frame.phase === 'upload') {
        const sample = frame as BandwidthSample
        const sec = sample.elapsed_ms / 1000
        if (sample.phase === 'download') {
          liveDown.value = sample.mbps
          downSamples.value.push({ x: sec, y: sample.mbps })
          phaseText.value = '正在测下行…'
        } else {
          liveUp.value = sample.mbps
          upSamples.value.push({ x: sec, y: sample.mbps })
          phaseText.value = '正在测上行…'
        }
      } else if (frame.phase === 'done') {
        result.value = {
          available: frame.available,
          latency: 0,
          mode: 'bandwidth',
          down_mbps: frame.down_mbps,
          up_mbps: frame.up_mbps,
          elapsed_ms: frame.elapsed_ms,
          min_down_mbps: frame.min_down_mbps,
          min_up_mbps: frame.min_up_mbps,
          error: frame.error
        }
        finished = true
        es?.close()
        es = null
        running.value = false
      } else if (frame.phase === 'error') {
        ElMessage.error(frame.error || '带宽测试失败')
        finished = true
        es?.close()
        es = null
        running.value = false
        visible.value = false
      }
    } catch (err) {
      console.error('parse SSE frame error', err)
    }
  }

  es.onerror = () => {
    if (!finished) {
      ElMessage.error('连接断开')
      es?.close()
      es = null
      running.value = false
      visible.value = false
    }
  }
}

const rerun = () => {
  downSamples.value = []
  upSamples.value = []
  liveDown.value = 0
  liveUp.value = 0
  runTest()
}

const onClosed = () => {
  es?.close()
  es = null
  if (!running.value) {
    result.value = null
    downSamples.value = []
    upSamples.value = []
  }
}

defineExpose({ open })
</script>

<style scoped>
.bw-testing {
  padding: 8px 0;
}
.bw-live {
  display: flex;
  gap: 24px;
  justify-content: center;
  margin-bottom: 12px;
}
.bw-live-item {
  text-align: center;
}
.bw-live-label {
  display: block;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}
.bw-live-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--el-color-primary);
  display: inline-block;
  min-width: 60px;
}
.bw-live-unit {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: 4px;
}
.bw-phase {
  text-align: center;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-top: 8px;
}
.bw-result {
  padding: 8px 0;
}
.bw-cards {
  display: flex;
  gap: 16px;
  justify-content: center;
}
.bw-card {
  flex: 1;
  text-align: center;
  padding: 20px 8px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-light);
}
.bw-card-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}
.bw-card-value {
  font-size: 32px;
  font-weight: 700;
  line-height: 1;
  color: var(--el-color-primary);
}
.bw-card-unit {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
.bw-meta {
  margin-top: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
}
.bw-threshold {
  font-size: 12px;
  opacity: 0.85;
}
.bw-elapsed {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
