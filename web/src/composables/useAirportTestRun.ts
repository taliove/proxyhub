import { computed, ref, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import { getJob, getJobResult, cancelJob, JOB_KIND_AIRPORT_TEST, type JobStatus } from '@/api/jobs'
import {
  runAirportTest,
  emptyDiagnostic,
  parseDiagnosticResult,
  parseCompletedResult,
  parseAirportTestCursor,
  getDiagnosticState,
  type DiagnosticResult,
  type CheckingProgress,
  type TestRun,
  type TestRunStatus,
  type DiagnosticState
} from './useAirportTest'

/**
 * useAirportTestRun 机场测试一次运行的状态机(自 AirportTestDialog 抽取,ESLint max-lines 瘦身):
 * start() 发起(POST test 拿 jobs 句柄)-> startPolling 轮询 /jobs/{id} cursor
 * 分阶段推进(applyCursor)-> jobs 终态收口(applyTerminal,/jobs/{id}/result 取 run 行)。
 * 数据源口径见 ADR 0027;纯解析工具仍在 useAirportTest.ts,本文件只管运行流程。
 * onFinished 在 run 到达 completed 终态时回调(父级刷新列表与抽屉报告)。
 */
export function useAirportTestRun(onFinished: () => void) {
  const airport = ref<Airport | null>(null)
  // 本次运行模式(发起时的 full 参数;ticket 0043 抽样语义展示)
  const testFull = ref(false)
  const phase = ref<TestRunStatus>('diagnosing')
  const diagnosticState = ref<DiagnosticState>('success')
  const diagnosticReady = ref(false)
  const cancelling = ref(false)
  const diagnosticResult = ref<DiagnosticResult>(emptyDiagnostic())
  const checkingProgress = ref<CheckingProgress | null>(null)
  // 终态兜底的涉及节点数:秒级完成的运行(抽样浅测主场景)轮询直接看到终态,
  // checkingProgress 从未赋值,此时从 run 维度取(抽样=sampled_nodes 数,全量=total_nodes)
  const terminalInvolvedCount = ref<number | null>(null)
  const overallScore = ref<number>(0)
  const errorMessage = ref('')
  const currentJobId = ref<number | null>(null)
  const currentJobKey = ref('')
  const pollingTimer = ref<number | null>(null)

  const isRunningPhase = computed(
    () => phase.value === 'diagnosing' || phase.value === 'checking' || phase.value === 'scoring'
  )

  // 本次涉及节点数:优先检活 cursor 的 total;轮询未及看到 checking 阶段的
  // 秒级运行用终态兜底(见 terminalInvolvedCount);诊断阶段为 null
  const involvedCount = computed(() => {
    if (checkingProgress.value && checkingProgress.value.total > 0) {
      return checkingProgress.value.total
    }
    return terminalInvolvedCount.value
  })

  const stopPolling = () => {
    if (pollingTimer.value) {
      clearInterval(pollingTimer.value)
      pollingTimer.value = null
    }
  }

  // 显式运行入口:父级(机场管理页/详情抽屉)在用户点「测试」/「重新测试」/「测全部」时调用
  const start = (target: Airport, full = false) => {
    airport.value = target
    testFull.value = full
    void startTest(full)
  }

  const startTest = async (full: boolean) => {
    if (!airport.value) return

    phase.value = 'diagnosing'
    errorMessage.value = ''
    diagnosticReady.value = false
    stopPolling()

    try {
      const handle = await runAirportTest(airport.value.id, full)
      currentJobId.value = handle.jobId
      currentJobKey.value = handle.key
      startPolling()
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
      // 秒级完成兜底:轮询未看到 checking 阶段时,从 run 维度取本次检活数
      const completed = parseCompletedResult(run.dimensions_json)
      if (completed) {
        terminalInvolvedCount.value = testFull.value
          ? completed.total_nodes
          : (completed.sampled_nodes?.length ?? completed.total_nodes)
      }
    }

    switch (status) {
      case 'done':
        if (run) {
          handleCompletedRun(run)
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

  // cancel 取消进行中任务(jobs 通用取消端点);取消后由轮询观察到 cancelled 终态收口。
  const cancel = async () => {
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

  const handleCompletedRun = (run: TestRun) => {
    phase.value = 'completed'
    overallScore.value = run.overall_score ?? 0
    if (!parseCompletedResult(run.dimensions_json)) {
      // 兜底:completed 但维度缺失(不应发生),仍按分数呈现结论
      console.warn('completed run missing score dimensions', run.id)
    }
    onFinished()
  }

  // reset 对话框关闭时复位全部运行状态(visible 归对话框自管)
  const reset = () => {
    stopPolling()
    phase.value = 'diagnosing'
    currentJobId.value = null
    currentJobKey.value = ''
    airport.value = null
    testFull.value = false
    cancelling.value = false
    diagnosticReady.value = false
    diagnosticResult.value = emptyDiagnostic()
    checkingProgress.value = null
    terminalInvolvedCount.value = null
    overallScore.value = 0
    errorMessage.value = ''
  }

  onUnmounted(stopPolling)

  return {
    airport,
    testFull,
    phase,
    diagnosticState,
    diagnosticReady,
    cancelling,
    diagnosticResult,
    checkingProgress,
    overallScore,
    errorMessage,
    isRunningPhase,
    involvedCount,
    start,
    cancel,
    reset,
    stopPolling
  }
}
