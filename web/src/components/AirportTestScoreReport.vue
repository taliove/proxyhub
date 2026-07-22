<template>
  <div class="score-report">
    <div class="score-header">
      <div class="overall-score">
        <StatusDot :tone="scoreTone" :label="scoreToneLabel" />
        <div class="score-label">综合得分</div>
        <el-tag :type="scoreColor" size="large" class="score-value num">
          {{ overallScore }}
        </el-tag>
      </div>
      <el-button type="primary" size="small" @click="emit('runFull')">跑全量检测</el-button>
    </div>

    <el-divider />

    <h4>📊 诊断结果</h4>
    <el-descriptions :column="2" border size="small" class="compact-descriptions num">
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
      <el-descriptions :column="1" border size="small" class="num">
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
import {
  scoreTone as scoreToneOf,
  scoreToneLabel as scoreToneLabelOf
} from '@/views/airport-test-utils'
import StatusDot from '@/components/StatusDot.vue'

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
const scoreTone = computed(() => scoreToneOf(props.overallScore))
const scoreToneLabel = computed(() => scoreToneLabelOf(props.overallScore))
</script>

<style scoped>
.score-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--ph-space-4);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-sm);
}

.overall-score {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
}

.score-label {
  font-size: var(--ph-text-md);
  font-weight: 500;
}

.score-value {
  font-size: var(--ph-text-2xl);
  font-weight: bold;
  padding: var(--ph-space-2) var(--ph-space-4);
}

.num {
  font-variant-numeric: tabular-nums;
}

.compact-descriptions {
  margin-bottom: var(--ph-space-5);
}

.score-details {
  margin-top: var(--ph-space-5);
}

.score-details h4 {
  margin-bottom: var(--ph-space-3);
}

.dimension-score {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  margin-left: var(--ph-space-2);
}
</style>
