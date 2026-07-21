<template>
  <div class="score-report">
    <div class="score-header">
      <div class="overall-score">
        <div class="score-label">综合得分</div>
        <el-tag :type="scoreColor" size="large" class="score-value">
          {{ overallScore }}
        </el-tag>
      </div>
      <el-button type="primary" size="small" @click="emit('runFull')">跑全量检测</el-button>
    </div>

    <el-divider />

    <h4>📊 诊断结果</h4>
    <el-descriptions :column="2" border size="small" class="compact-descriptions">
      <el-descriptions-item label="HTTP 状态">
        <el-tag :type="diagnostic.http_status === 200 ? 'success' : 'danger'" size="small">
          {{ diagnostic.http_status }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="拉取耗时">
        {{ diagnostic.duration_ms }} ms
      </el-descriptions-item>
      <el-descriptions-item label="解析成功">
        {{ diagnostic.node_count }} 节点
      </el-descriptions-item>
      <el-descriptions-item label="解析失败">
        <el-tag v-if="diagnostic.parse_failures > 0" type="warning" size="small">
          {{ diagnostic.parse_failures }} 行
        </el-tag>
        <span v-else>0</span>
      </el-descriptions-item>
    </el-descriptions>

    <div v-if="completedResult" class="score-details">
      <h4>📈 维度明细</h4>
      <el-descriptions :column="1" border size="small">
        <el-descriptions-item label="可用率">
          {{ (completedResult.availability_rate * 100).toFixed(1) }}%
          <span class="dimension-score"
            >({{ completedResult.score_breakdown.availability_score.toFixed(1) }} 分)</span
          >
        </el-descriptions-item>
        <el-descriptions-item label="延迟表现">
          平均 {{ completedResult.latency_mean_ms.toFixed(0) }} ms / P95
          {{ completedResult.latency_p95_ms.toFixed(0) }} ms
          <span class="dimension-score"
            >({{ completedResult.score_breakdown.latency_score.toFixed(1) }} 分)</span
          >
        </el-descriptions-item>
        <el-descriptions-item label="拉取健康">
          <span class="dimension-score"
            >{{ completedResult.score_breakdown.fetch_health_score.toFixed(1) }} 分</span
          >
        </el-descriptions-item>
        <el-descriptions-item label="地区覆盖">
          {{ completedResult.region_coverage_count }} 个地区
          <span class="dimension-score"
            >({{ completedResult.score_breakdown.region_coverage_score.toFixed(1) }} 分)</span
          >
        </el-descriptions-item>
      </el-descriptions>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  getScoreColor,
  type DiagnosticResult,
  type CompletedResult
} from '@/composables/useAirportTest'

interface Props {
  overallScore: number
  diagnostic: DiagnosticResult
  completedResult: CompletedResult | null
}

interface Emits {
  (e: 'runFull'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const scoreColor = computed(() => getScoreColor(props.overallScore))
</script>

<style scoped>
.score-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}

.overall-score {
  display: flex;
  align-items: center;
  gap: 12px;
}

.score-label {
  font-size: 16px;
  font-weight: 500;
}

.score-value {
  font-size: 24px;
  font-weight: bold;
  padding: 8px 16px;
}

.compact-descriptions {
  margin-bottom: 20px;
}

.score-details {
  margin-top: 20px;
}

.score-details h4 {
  margin-bottom: 12px;
}

.dimension-score {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-left: 8px;
}
</style>
