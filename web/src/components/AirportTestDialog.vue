<template>
  <el-dialog
    v-model="visible"
    :title="`机场测试 - ${airport?.name || ''}`"
    width="700px"
    @close="handleClose"
  >
    <!-- Diagnostics phase -->
    <div v-if="phase === 'diagnosing'" class="test-phase">
      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在诊断订阅...</span>
      </div>
    </div>

    <!-- Checking phase (with progress) -->
    <div v-else-if="phase === 'checking'" class="test-phase">
      <h4>✅ 诊断完成</h4>

      <!-- Success state: URL reachable -->
      <el-descriptions
        v-if="diagnosticState === 'success'"
        :column="2"
        border
        size="small"
        class="compact-descriptions num"
      >
        <el-descriptions-item label="HTTP 状态">
          <el-tag :type="diagnosticResult.http_status === 200 ? 'success' : 'danger'" size="small">
            {{ diagnosticResult.http_status }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="耗时">
          {{ diagnosticResult.duration_ms }} ms
        </el-descriptions-item>
        <el-descriptions-item label="解析成功">
          {{ diagnosticResult.node_count }} 节点
        </el-descriptions-item>
        <el-descriptions-item label="解析失败">
          <el-tag v-if="diagnosticResult.parse_failures > 0" type="warning" size="small">
            {{ diagnosticResult.parse_failures }} 行
          </el-tag>
          <span v-else>0</span>
        </el-descriptions-item>
      </el-descriptions>

      <!-- Unreachable state: URL unreachable but pool has nodes -->
      <el-alert
        v-else-if="diagnosticState === 'unreachable'"
        type="warning"
        :closable="false"
        show-icon
        class="diagnostic-alert"
      >
        <template #title>订阅 URL 当前不可达</template>
        已基于池内已同步节点进行测试,评分不含拉取健康维度(权重重归一)。
      </el-alert>

      <el-divider />

      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在检活节点...</span>
      </div>
      <el-progress
        v-if="checkingProgress"
        :percentage="Math.round((checkingProgress.checked / checkingProgress.total) * 100)"
        :format="() => `${checkingProgress?.checked || 0} / ${checkingProgress?.total || 0}`"
      />
    </div>

    <!-- Scoring phase -->
    <div v-else-if="phase === 'scoring'" class="test-phase">
      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在计算综合评分...</span>
      </div>
    </div>

    <!-- Completed phase (full report) -->
    <div v-else-if="phase === 'completed'" class="test-phase">
      <AirportTestScoreReport
        :overall-score="overallScore"
        :diagnostic="diagnosticResult"
        :completed-result="completedResult"
        @run-full="runFullTest"
      />

      <AirportTestTrend :runs="historyRuns" />
    </div>

    <!-- Error state -->
    <div v-else-if="phase === 'failed'" class="test-phase">
      <el-alert type="error" :closable="false" show-icon>
        <template #title>测试失败</template>
        {{ errorMessage }}
      </el-alert>
    </div>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import {
  runAirportTest,
  getTestRun,
  listTestRuns,
  parseDiagnosticResult,
  parseCheckingProgress,
  parseCompletedResult,
  getDiagnosticState,
  type DiagnosticResult,
  type CheckingProgress,
  type CompletedResult,
  type TestRun,
  type TestRunStatus,
  type DiagnosticState
} from '@/composables/useAirportTest'
import AirportTestScoreReport from './AirportTestScoreReport.vue'
import AirportTestTrend from './AirportTestTrend.vue'

interface Props {
  modelValue: boolean
  airport: Airport | null
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = ref(false)
const phase = ref<TestRunStatus>('diagnosing')
const diagnosticState = ref<DiagnosticState>('success')
const currentRunId = ref<number | null>(null)
const diagnosticResult = ref<DiagnosticResult>({
  http_status: 0,
  duration_ms: 0,
  node_count: 0,
  protocol_counts: {},
  parse_failures: 0
})
const checkingProgress = ref<CheckingProgress | null>(null)
const completedResult = ref<CompletedResult | null>(null)
const overallScore = ref<number>(0)
const errorMessage = ref('')
const historyRuns = ref<TestRun[]>([])
const pollingTimer = ref<number | null>(null)

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val && props.airport) {
      startTest()
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
  if (!val) {
    stopPolling()
  }
})

const startTest = async (full = false) => {
  if (!props.airport) return

  phase.value = 'diagnosing'
  errorMessage.value = ''
  stopPolling()

  try {
    const result = await runAirportTest(props.airport.id, full)
    currentRunId.value = result.id

    if (result.status === 'failed') {
      phase.value = 'failed'
      errorMessage.value = result.error_message || '未知错误'
      return
    }

    const dims = parseDiagnosticResult(result.dimensions_json)
    diagnosticResult.value = dims

    if (result.status !== 'completed') {
      startPolling()
    } else {
      handleCompletedRun(result)
    }

    loadHistory()
  } catch (error) {
    const err = error as { response?: { data?: { error?: string } }; message?: string }
    phase.value = 'failed'
    errorMessage.value = err.response?.data?.error || err.message || '请求失败'
    ElMessage.error('测试执行失败')
  }
}

const runFullTest = () => {
  startTest(true)
}

const startPolling = () => {
  stopPolling()

  const poll = async () => {
    if (!props.airport || !currentRunId.value) return

    try {
      const run = await getTestRun(props.airport.id, currentRunId.value)

      // Update diagnostic state
      diagnosticState.value = getDiagnosticState(run.status, run.dimensions_json)

      if (run.status === 'failed') {
        phase.value = 'failed'
        errorMessage.value = run.error_message || '测试失败'
        stopPolling()
        return
      }

      if (run.status === 'checking') {
        phase.value = 'checking'
        const progress = parseCheckingProgress(run.dimensions_json)
        if (progress) {
          checkingProgress.value = progress
        }
      } else if (run.status === 'scoring') {
        phase.value = 'scoring'
      } else if (run.status === 'completed') {
        handleCompletedRun(run)
        stopPolling()
        loadHistory()
      }
    } catch (error) {
      console.error('Polling error:', error)
    }
  }

  pollingTimer.value = window.setInterval(poll, 1500)
}

const stopPolling = () => {
  if (pollingTimer.value) {
    clearInterval(pollingTimer.value)
    pollingTimer.value = null
  }
}

const handleCompletedRun = (run: TestRun) => {
  phase.value = 'completed'
  const result = parseCompletedResult(run.dimensions_json)
  if (result) {
    completedResult.value = result
    overallScore.value = run.overall_score || 0
  }
}

const loadHistory = async () => {
  if (!props.airport) return

  try {
    const runs = await listTestRuns(props.airport.id)
    historyRuns.value = runs
  } catch (error) {
    console.error('Failed to load history:', error)
  }
}

const handleClose = () => {
  stopPolling()
  phase.value = 'diagnosing'
  currentRunId.value = null
  diagnosticResult.value = {
    http_status: 0,
    duration_ms: 0,
    node_count: 0,
    protocol_counts: {},
    parse_failures: 0
  }
  checkingProgress.value = null
  completedResult.value = null
  overallScore.value = 0
  errorMessage.value = ''
  historyRuns.value = []
}

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.test-phase {
  min-height: 200px;
}

.num {
  font-variant-numeric: tabular-nums;
}

.phase-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-5) 0;
  font-size: var(--ph-text-md);
  color: var(--ph-text-secondary);
}

.phase-loading .el-icon {
  font-size: var(--ph-text-2xl);
}

.compact-descriptions {
  margin-bottom: var(--ph-space-5);
}

.diagnostic-alert {
  margin-bottom: var(--ph-space-5);
}
</style>
