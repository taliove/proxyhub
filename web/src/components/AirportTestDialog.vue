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
      <AirportTestDiagnostic v-if="diagnosticState === 'success'" :diagnostic="diagnosticResult" />

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

    <!-- Cancelled state (已写回的检活结果保留,诊断数据可见) -->
    <div v-else-if="phase === 'cancelled'" class="test-phase">
      <el-alert type="info" :closable="false" show-icon class="diagnostic-alert">
        <template #title>测试已取消</template>
        已写回的节点检活结果保留,未产生评分报告。
      </el-alert>
      <template v-if="diagnosticReady">
        <h4>📊 诊断结果(取消前已产出)</h4>
        <AirportTestDiagnostic :diagnostic="diagnosticResult" />
      </template>
    </div>

    <!-- Error state -->
    <div v-else-if="phase === 'failed'" class="test-phase">
      <el-alert type="error" :closable="false" show-icon>
        <template #title>测试失败</template>
        {{ errorMessage }}
      </el-alert>
    </div>

    <template #footer>
      <el-button v-if="isRunningPhase" type="warning" :loading="cancelling" @click="onCancel">
        取消测试
      </el-button>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import { getJob, getJobResult, cancelJob, JOB_KIND_AIRPORT_TEST, type JobStatus } from '@/api/jobs'
import {
  runAirportTest,
  listTestRuns,
  emptyDiagnostic,
  parseDiagnosticResult,
  parseCompletedResult,
  parseAirportTestCursor,
  getDiagnosticState,
  type DiagnosticResult,
  type CheckingProgress,
  type CompletedResult,
  type TestRun,
  type TestRunStatus,
  type DiagnosticState
} from '@/composables/useAirportTest'
import AirportTestScoreReport from './AirportTestScoreReport.vue'
import AirportTestDiagnostic from './AirportTestDiagnostic.vue'
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

// 数据源(issue 0026,ADR 0027):POST /airports/{id}/test 返回任务句柄 {jobId,kind,key};
// 进度主源 = 轮询 /api/jobs/{jobId} 的 cursor({"phase","checked","total"});
// 终态由 jobs status 驱动;诊断与报告走 /api/jobs/{jobId}/result 的 airport_test_run。
const visible = ref(false)
const phase = ref<TestRunStatus>('diagnosing')
const diagnosticState = ref<DiagnosticState>('success')
const diagnosticReady = ref(false)
const currentJobId = ref<number | null>(null)
const currentJobKey = ref('')
const cancelling = ref(false)
const diagnosticResult = ref<DiagnosticResult>(emptyDiagnostic())
const checkingProgress = ref<CheckingProgress | null>(null)
const completedResult = ref<CompletedResult | null>(null)
const overallScore = ref<number>(0)
const errorMessage = ref('')
const historyRuns = ref<TestRun[]>([])
const pollingTimer = ref<number | null>(null)

const isRunningPhase = computed(
  () => phase.value === 'diagnosing' || phase.value === 'checking' || phase.value === 'scoring'
)

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
  diagnosticReady.value = false
  stopPolling()

  try {
    const handle = await runAirportTest(props.airport.id, full)
    currentJobId.value = handle.jobId
    currentJobKey.value = handle.key
    startPolling()
    loadHistory()
  } catch (error) {
    const err = error as { response?: { status?: number; data?: unknown }; message?: string }
    phase.value = 'failed'
    // 409 = 同机场刷新在跑(跨 kind 互斥);其余错误后端为纯文本或拦截器已提示
    errorMessage.value =
      err.response?.status === 409
        ? '同机场有刷新任务进行中,暂不能发起测试'
        : typeof err.response?.data === 'string'
          ? err.response.data
          : err.message || '请求失败'
    ElMessage.error('测试发起失败')
  }
}

const runFullTest = () => {
  startTest(true)
}

const startPolling = () => {
  stopPolling()

  const poll = async () => {
    const jobId = currentJobId.value
    if (!jobId) return

    try {
      const job = await getJob(jobId)

      if (job.status === 'running') {
        applyCursor(job.cursor)
        return
      }

      // 终态(done/failed/cancelled/interrupted):停轮询,取 run 行收口展示
      stopPolling()
      await applyTerminal(job.status)
    } catch (error) {
      console.error('Polling error:', error)
    }
  }

  pollingTimer.value = window.setInterval(poll, 1500)
}

// applyCursor 按 cursor 阶段推进分阶段 UX;checking 阶段带 checked/total 进度。
const applyCursor = (cursorStr?: string) => {
  const cursor = parseAirportTestCursor(cursorStr)
  if (!cursor) {
    phase.value = 'diagnosing'
    return
  }
  if (cursor.phase === 'checking') {
    phase.value = 'checking'
    if (cursor.total > 0) {
      checkingProgress.value = { checked: cursor.checked, total: cursor.total }
    }
    void ensureDiagnostic()
  } else if (cursor.phase === 'scoring') {
    phase.value = 'scoring'
    void ensureDiagnostic()
  } else {
    phase.value = 'diagnosing'
  }
}

// ensureDiagnostic 进入检活/评分阶段后拉一次 run 行取诊断数据
// (run 行建行即带诊断;拉取期间 run 未建行,结果端点回 no_report,下次轮询再试)。
const ensureDiagnostic = async () => {
  if (diagnosticReady.value || !currentJobId.value) return
  try {
    const res = await getJobResult(currentJobId.value)
    const run = res.airport_test_run
    if (!run) return
    diagnosticResult.value = parseDiagnosticResult(run.dimensions_json)
    diagnosticState.value = getDiagnosticState(run.status, run.dimensions_json)
    diagnosticReady.value = true
  } catch {
    // 诊断拉取失败不阻塞进度展示,下阶段/终态再取
  }
}

const applyTerminal = async (status: JobStatus) => {
  let run: TestRun | null = null
  if (currentJobId.value) {
    try {
      const res = await getJobResult(currentJobId.value)
      run = res.airport_test_run ?? null
    } catch {
      // 结果端点失败:按 jobs 状态兜底展示
    }
  }

  if (run) {
    diagnosticResult.value = parseDiagnosticResult(run.dimensions_json)
    diagnosticState.value = getDiagnosticState(run.status, run.dimensions_json)
    diagnosticReady.value = true
  }

  switch (status) {
    case 'done':
      if (run) {
        handleCompletedRun(run)
        loadHistory()
      } else {
        phase.value = 'failed'
        errorMessage.value = '未找到本次测试报告'
      }
      break
    case 'cancelled':
      phase.value = 'cancelled'
      break
    case 'interrupted':
      phase.value = 'failed'
      errorMessage.value = '任务已被中断(进程重启),未产生完整报告'
      break
    default:
      phase.value = 'failed'
      errorMessage.value = run?.error_message || '测试失败'
  }
}

// onCancel 取消进行中任务(jobs 通用取消端点,kind=airport_test);
// 取消后由轮询观察到 cancelled 终态收口。
const onCancel = async () => {
  if (!currentJobKey.value) return
  cancelling.value = true
  try {
    await cancelJob(JOB_KIND_AIRPORT_TEST, currentJobKey.value)
    ElMessage.success('已发送取消')
  } catch {
    // 409 = 任务已结束,轮询会纠正视图
  } finally {
    cancelling.value = false
  }
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
  currentJobId.value = null
  currentJobKey.value = ''
  cancelling.value = false
  diagnosticReady.value = false
  diagnosticResult.value = emptyDiagnostic()
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

.diagnostic-alert {
  margin-bottom: var(--ph-space-5);
}
</style>
