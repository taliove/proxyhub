<template>
  <div class="test-report">
    <div class="report-header">
      <div class="drawer-section-title">最近测试</div>
      <!-- 重跑是显式动作:只上抛意图,由父级打开运行模式对话框;查看本报告不产生新 run -->
      <div v-if="!readonly" class="report-actions">
        <el-button size="small" type="primary" @click="emit('run-test', false)">重新测试</el-button>
        <el-button size="small" @click="emit('run-test', true)">测全部</el-button>
      </div>
    </div>

    <div v-if="loading" class="report-loading muted">加载测试记录…</div>

    <!-- 空态:从未测过 -->
    <div v-else-if="runs.length === 0" class="report-empty muted">
      尚未测试过。点「重新测试」做分层抽样检活,或「测全部」全量检活。
    </div>

    <template v-else>
      <!-- 失败态:最近一次 run failed(池空且订阅 URL 不通) -->
      <el-alert
        v-if="latestRun && latestRun.status === 'failed'"
        type="error"
        :closable="false"
        show-icon
        class="report-alert"
      >
        <template #title>上次测试失败({{ relativeTime(latestRun.created_at) }})</template>
        {{ latestRun.error_message || '未知错误' }}
      </el-alert>

      <template v-if="completedRun && completedResult">
        <!-- 综合评分头部:分数 + 时间 + 抽样/全量口径 -->
        <div class="score-header">
          <div class="overall-score">
            <StatusDot :tone="scoreToneOf(overallScore)" :label="scoreToneLabelOf(overallScore)" />
            <span class="score-label">综合得分</span>
            <el-tag :type="getScoreColor(overallScore)" size="large" class="score-value num">
              {{ overallScore.toFixed(1) }}
            </el-tag>
            <el-tag size="small" type="info" class="mode-tag">
              {{ completedRun.is_full ? '全量' : '抽样' }}
            </el-tag>
          </div>
          <span class="muted">{{ relativeTime(completedRun.created_at) }}</span>
        </div>

        <!-- URL 不可达口径说明:拉取健康 N/A,权重按 5:3:1 重归一 -->
        <el-alert
          v-if="!completedResult.url_reachable"
          type="warning"
          :closable="false"
          show-icon
          class="report-alert"
        >
          <template #title>该次测试订阅 URL 不可达</template>
          基于池内已同步节点评分;拉取健康维度 N/A,权重按 5:3:1 重归一到其余维度。
        </el-alert>

        <!-- 事实汇总 -->
        <el-descriptions :column="2" border size="small" class="report-block num">
          <el-descriptions-item label="可用节点">
            {{ completedResult.available_nodes }} / {{ completedResult.total_nodes }}
          </el-descriptions-item>
          <el-descriptions-item label="平均延迟">
            {{ completedResult.mean_latency_ms.toFixed(0) }} ms(P95
            {{ completedResult.p95_latency_ms.toFixed(0) }} ms)
          </el-descriptions-item>
          <el-descriptions-item label="地区覆盖">
            {{ completedResult.region_count }} 个地区
            <span v-if="regionList" class="muted">({{ regionList }})</span>
          </el-descriptions-item>
          <el-descriptions-item label="拉取健康">
            <template v-if="completedResult.url_reachable">
              HTTP {{ completedResult.http_status }},解析成功率
              {{ (completedResult.parse_success_rate * 100).toFixed(1) }}%
            </template>
            <span v-else class="muted">N/A(URL 不可达)</span>
          </el-descriptions-item>
        </el-descriptions>

        <!-- 维度构成拆解:打开黑盒,直接给出各维度得分与权重 -->
        <div class="report-subtitle">评分构成</div>
        <el-descriptions :column="1" border size="small" class="report-block num">
          <el-descriptions-item :label="`可用率(权重 ${weightLabel(weights.availability)})`">
            {{ completedResult.availability_score.toFixed(1) }} 分
          </el-descriptions-item>
          <el-descriptions-item :label="`延迟表现(权重 ${weightLabel(weights.latency)})`">
            {{ completedResult.latency_score.toFixed(1) }} 分
          </el-descriptions-item>
          <el-descriptions-item
            :label="
              weights.fetchHealth === null
                ? '拉取健康(N/A,权重已重归一)'
                : `拉取健康(权重 ${weightLabel(weights.fetchHealth)})`
            "
          >
            <span v-if="weights.fetchHealth === null" class="muted">N/A</span>
            <span v-else>{{ completedResult.fetch_health_score.toFixed(1) }} 分</span>
          </el-descriptions-item>
          <el-descriptions-item :label="`地区覆盖(权重 ${weightLabel(weights.region)})`">
            {{ completedResult.region_score.toFixed(1) }} 分
          </el-descriptions-item>
        </el-descriptions>

        <!-- 抽样节点明细:每节点可用性/延迟;旧 run 未持久化明细则降级为汇总 -->
        <div class="report-subtitle">抽样节点明细</div>
        <el-table
          v-if="sampledNodes.length > 0"
          :data="sampledNodes"
          size="small"
          border
          class="report-block"
        >
          <el-table-column label="名称" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ row.name }}</template>
          </el-table-column>
          <el-table-column label="地区" width="72">
            <template #default="{ row }">{{ regionDisplay(row.region) }}</template>
          </el-table-column>
          <el-table-column label="可用性" width="100">
            <template #default="{ row }">
              <StatusDot
                :tone="row.available ? 'success' : 'danger'"
                :label="row.available ? '可用' : '不可用'"
                class="sample-dot"
              />
              <span>{{ row.available ? '可用' : '不可用' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="延迟" width="80">
            <template #default="{ row }">
              <span class="num">{{ row.available ? `${row.latency_ms}ms` : '—' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="muted report-block">
          该次测试记录较早,未保存抽样节点明细,仅显示汇总。
        </div>
      </template>

      <!-- 历史趋势(复用既有组件,只看 scored run) -->
      <AirportTestTrend :runs="runs" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  getScoreColor,
  parseCompletedResult,
  dimensionWeights,
  weightLabel,
  type TestRun
} from '@/composables/useAirportTest'
import {
  scoreTone as scoreToneOf,
  scoreToneLabel as scoreToneLabelOf,
  testTimeRelative
} from '@/views/airport-test-utils'
import { regionDisplay } from '@/views/nodes/nodecells'
import StatusDot from '@/components/StatusDot.vue'
import AirportTestTrend from '@/components/AirportTestTrend.vue'

// 纯展示组件:数据由父级(详情抽屉)拉取并传入;重跑意图上抛,自身不发任何请求。
// readonly=true 时隐藏重跑入口(任务详情等只读场景,对齐旧 ScoreReport 的 showRunFull=false)。
const props = withDefaults(
  defineProps<{
    runs: TestRun[]
    loading?: boolean
    readonly?: boolean
  }>(),
  { loading: false, readonly: false }
)

const emit = defineEmits<{
  // full=false 抽样重跑;full=true 测全部
  (e: 'run-test', full: boolean): void
}>()

const latestRun = computed(() => (props.runs.length > 0 ? props.runs[0] : null))

// 最近一次 completed run:报告主体;列表按 id 倒序,第一个 completed 即最近
const completedRun = computed(() => props.runs.find((r) => r.status === 'completed') ?? null)

const completedResult = computed(() =>
  completedRun.value ? parseCompletedResult(completedRun.value.dimensions_json) : null
)

const overallScore = computed(() => completedRun.value?.overall_score ?? 0)

const weights = computed(() => dimensionWeights(completedResult.value?.url_reachable ?? true))

const regionList = computed(() => {
  const dist = completedResult.value?.region_distribution
  if (!dist) return ''
  return Object.keys(dist).sort().join('/')
})

const sampledNodes = computed(() => completedResult.value?.sampled_nodes ?? [])

const relativeTime = (iso: string): string => testTimeRelative(iso)
</script>

<style scoped>
.report-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--ph-space-2);
}
.drawer-section-title {
  font-weight: 600;
}
.report-actions {
  flex: none;
}
.report-loading,
.report-empty {
  padding: var(--ph-space-4) 0;
}
.report-alert {
  margin-bottom: var(--ph-space-3);
}
.report-block {
  margin-bottom: var(--ph-space-3);
}
.report-subtitle {
  font-weight: 600;
  font-size: var(--ph-text-sm);
  margin-bottom: var(--ph-space-2);
}
.score-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--ph-space-3) var(--ph-space-4);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-sm);
  margin-bottom: var(--ph-space-3);
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
  font-size: var(--ph-text-xl);
  font-weight: bold;
}
.mode-tag {
  flex: none;
}
.sample-dot {
  margin-right: var(--ph-space-1);
}
.num {
  font-variant-numeric: tabular-nums;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
