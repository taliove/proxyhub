<template>
  <div>
    <PageHeader
      description="本机实测：选择节点（或直连），后端经该节点透传测速数据到浏览器，测真实链路带宽与延迟，再与直连基线对比节点开销"
    />

    <el-card class="control-card">
      <div class="control-row">
        <el-select
          v-model="selectedKey"
          class="node-select"
          :loading="nodesLoading"
          :disabled="running"
          placeholder="选择本次链路节点"
          filterable
        >
          <el-option v-for="opt in options" :key="opt.value" :value="opt.value" :label="opt.label">
            <span>{{ opt.label }}</span>
            <span v-if="opt.region" class="option-region">{{ opt.region }}</span>
          </el-option>
        </el-select>
        <el-button v-if="!running" type="primary" @click="onStart">开始实测</el-button>
        <el-button v-else type="danger" @click="cancel">取消</el-button>
      </div>
      <div class="control-hint">
        选择要实测的节点（或留空表示直连），点「开始实测」：后端经该节点访问公共测速端点并透传数据到浏览器（8
        并行连接，流量可见于 DevTools Network），测出该节点的真实链路带宽、延迟与抖动。<br />
        历史列表右侧的 Δ
        列是各节点相对「直连」基线的开销。请先以留空状态测一次直连作为基线，之后各节点的 Δ
        才会有值。
      </div>
      <el-alert v-if="runError" type="error" :closable="false" class="run-error">
        {{ runError }}
      </el-alert>
    </el-card>

    <ResultCards
      :running="running"
      :phase="phase"
      :live-mbps="liveMbps"
      :down-final-mbps="downFinalMbps"
      :up-final-mbps="upFinalMbps"
      :idle-latency-ms="idleLatencyMs"
      :jitter-ms="jitterMs"
      :result="result"
    />

    <HistoryTable
      :rows="rows"
      :records="records"
      :loading="historyLoading"
      :label-of="labelOf"
      @delete="onDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { saveSpeedtestResult } from '@/api/speedtest'
import { round2 } from './utils'
import { useSpeedtestRun } from './composables/useSpeedtestRun'
import { useSpeedtestHistory } from './composables/useSpeedtestHistory'
import { useNodeAnnotation } from './composables/useNodeAnnotation'
import ResultCards from './components/ResultCards.vue'
import HistoryTable from './components/HistoryTable.vue'

const route = useRoute()
const {
  selectedKey,
  options,
  loading: nodesLoading,
  labelOf,
  poolKeys,
  load: loadAnnotation,
  applyQuery
} = useNodeAnnotation()
const {
  records,
  rows,
  loading: historyLoading,
  load: loadHistory,
  remove
} = useSpeedtestHistory(poolKeys)
const {
  phase,
  running,
  liveMbps,
  downFinalMbps,
  upFinalMbps,
  idleLatencyMs,
  jitterMs,
  result,
  error: runError,
  start,
  cancel
} = useSpeedtestRun()

// CLIENT_INFO_MAX 与后端 maxSpeedtestClientInfoLen(512)对齐,留余量截断。
const CLIENT_INFO_MAX = 500

// 一键实测:跑完自动落库(node_key = 当前标注,'' = 直连)并刷新历史。
const onStart = async () => {
  const outcome = await start(selectedKey.value)
  if (!outcome) return
  try {
    await saveSpeedtestResult({
      node_key: selectedKey.value,
      down_mbps: round2(outcome.downMbps),
      up_mbps: round2(outcome.upMbps),
      idle_latency_ms: round2(outcome.idleLatencyMs),
      jitter_ms: round2(outcome.jitterMs),
      client_info: navigator.userAgent.slice(0, CLIENT_INFO_MAX)
    })
    ElMessage.success('实测完成,已保存到历史')
  } catch {
    // 落库失败不吞测量结果:大数字已展示,历史不刷新(client.ts 拦截器已报错)
    return
  }
  await loadHistory()
}

const onDelete = async (id: number) => {
  await remove(id)
  ElMessage.success('已删除')
}

// 预填:?node_key= 直达(节点表/详情抽屉入口);节点已不在池时提示回落,学 /jobs?id= 模式。
onMounted(async () => {
  await Promise.all([loadAnnotation(), loadHistory()])
  if (applyQuery(route.query.node_key) === 'orphan') {
    ElMessage.warning('该节点已不在节点池,请重新标注或按直连实测')
  }
})
</script>

<style scoped>
.control-card {
  margin-bottom: var(--ph-space-4);
}
.control-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
}
.node-select {
  width: 320px;
}
.option-region {
  float: right;
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}
.control-hint {
  margin-top: var(--ph-space-2);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.run-error {
  margin-top: var(--ph-space-3);
}
</style>
