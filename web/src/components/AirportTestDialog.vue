<template>
  <el-dialog v-model="visible" width="700px" @close="handleClose">
    <!-- 头部:标题 + 运行态标签(诊断中/检活中/评分中/完成/已取消/失败),镜像深度体检对话框 -->
    <template #header>
      <div class="dialog-head">
        <span class="dialog-title">机场测试 · {{ airport?.name || '' }}</span>
        <el-tag :type="statusTag.type" size="small" effect="light">{{ statusTag.label }}</el-tag>
      </div>
    </template>

    <!-- 抽样语义副标题(ticket 0043):明示本次模式(抽样/全量)与涉及节点数,
         消除"测试秒结束=系统糊弄"的误解;节点数来自检活 cursor 的 total
         (本次实际检活数,抽样=N、全量=M,诊断阶段尚未产生时不显示) -->
    <div class="mode-line">
      <el-tag size="small" :type="testFull ? 'warning' : 'info'" class="mode-tag">
        {{ testFull ? '全量测试' : '抽样测试' }}
      </el-tag>
      <span v-if="involvedCount !== null" class="muted num">
        {{ testFull ? `共 ${involvedCount} 个节点` : `本次抽测 ${involvedCount} 个节点` }}
      </span>
    </div>

    <!-- 常驻三步流水线:诊断订阅 -> 检活节点 -> 综合评分。开测即全占位,阶段推进高亮当前步;
         终态(失败/取消)在停下的那步标红/标灰,把"走到哪一步"显性化,替代单一转圈圈。 -->
    <el-steps
      :active="activeStep"
      :process-status="stepProcessStatus"
      finish-status="success"
      align-center
      class="pipeline-steps"
    >
      <el-step title="诊断订阅" description="拉取并解析订阅" />
      <el-step title="检活节点" :description="checkStepDesc" />
      <el-step title="综合评分" description="加权汇总各维度" />
    </el-steps>

    <!-- Diagnostics phase -->
    <div v-if="phase === 'diagnosing'" class="test-phase">
      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在诊断订阅...</span>
      </div>
    </div>

    <!-- Checking phase (with progress) -->
    <div v-else-if="phase === 'checking'" class="test-phase">
      <!-- 诊断块一旦产出即常驻(检活/评分阶段延续可见) -->
      <AirportTestDiagnostic v-if="diagnosticState === 'success'" :diagnostic="diagnosticResult" />
      <el-alert
        v-else-if="diagnosticState === 'unreachable'"
        type="warning"
        :closable="false"
        show-icon
        class="diagnostic-alert"
      >
        <template #title>订阅 URL 当前不可达</template>
        已基于池内已同步节点进行测试，评分不含拉取健康维度（权重重归一）。
      </el-alert>

      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在检活节点...</span>
      </div>
      <!-- 进度带绝对计数 format(ticket 0044,对齐端点实测):checked / total -->
      <el-progress
        v-if="checkingProgress"
        :percentage="Math.round((checkingProgress.checked / checkingProgress.total) * 100)"
        :format="() => `${checkingProgress?.checked || 0} / ${checkingProgress?.total || 0}`"
      />
    </div>

    <!-- Scoring phase -->
    <div v-else-if="phase === 'scoring'" class="test-phase">
      <AirportTestDiagnostic v-if="diagnosticReady" :diagnostic="diagnosticResult" />
      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在计算综合评分...</span>
      </div>
    </div>

    <!-- Completed phase: 内嵌精简报告(ticket 0037 完整历史趋势/抽样明细仍归抽屉);
         得分头 title 带模式口径与检活节点数,主体为事实汇总 + 评分构成 -->
    <div v-else-if="phase === 'completed'" class="test-phase">
      <div class="score-header">
        <div class="overall-score">
          <span class="score-label">综合得分</span>
          <el-tag :type="getScoreColor(overallScore)" size="large" class="score-value num">
            {{ overallScore.toFixed(1) }}
          </el-tag>
        </div>
        <span class="muted">{{ completedTitle }}</span>
      </div>
      <AirportTestSummary v-if="completedResult" :result="completedResult" />
      <div v-else class="muted">本次未产出维度明细，仅呈现综合得分。</div>
      <div class="drawer-hint muted">完整历史趋势与抽样节点明细见机场详情抽屉「最近测试」。</div>
    </div>

    <!-- Cancelled state (已写回的检活结果保留,诊断数据可见) -->
    <div v-else-if="phase === 'cancelled'" class="test-phase">
      <el-alert type="info" :closable="false" show-icon class="diagnostic-alert">
        <template #title>{{ cancelledTitle }}</template>
        已写回的节点检活结果保留，未产生评分报告。
      </el-alert>
      <template v-if="diagnosticReady">
        <h4>📊 诊断结果（取消前已产出）</h4>
        <AirportTestDiagnostic :diagnostic="diagnosticResult" />
      </template>
    </div>

    <!-- Error state -->
    <div v-else-if="phase === 'failed'" class="test-phase">
      <el-alert type="error" :closable="false" show-icon>
        <template #title>{{ failedTitle }}</template>
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
import { ref, computed, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import type { Airport } from '@/types'
import { useAirportTestRun } from '@/composables/useAirportTestRun'
import { getScoreColor } from '@/composables/useAirportTest'
import AirportTestDiagnostic from './AirportTestDiagnostic.vue'
import AirportTestSummary from './AirportTestSummary.vue'

// 运行模式对话框(ticket 0037):只在父级显式调 start() 时才发起测试,
// 不再 watch 打开即跑;报告展示归详情抽屉「最近测试」段。
// 运行状态机(发起/轮询/终态收口)在 useAirportTestRun;本组件只留模板与薄接线。
interface Emits {
  // run 到达 completed 终态,父级刷新列表与抽屉报告
  (e: 'finished'): void
}

const emit = defineEmits<Emits>()

const visible = ref(false)

const {
  airport,
  testFull,
  phase,
  diagnosticState,
  diagnosticReady,
  cancelling,
  diagnosticResult,
  checkingProgress,
  overallScore,
  completedResult,
  errorMessage,
  isRunningPhase,
  involvedCount,
  start: startRun,
  cancel: onCancel,
  reset,
  stopPolling
} = useAirportTestRun(() => emit('finished'))

// 运行态标签:与深度体检对话框同构,把当前所处阶段/终态显性化在标题旁。
const statusTag = computed<{ label: string; type: 'success' | 'info' | 'warning' | 'danger' }>(
  () => {
    switch (phase.value) {
      case 'diagnosing':
        return { label: '诊断中', type: 'info' }
      case 'checking':
        return { label: '检活中', type: 'info' }
      case 'scoring':
        return { label: '评分中', type: 'info' }
      case 'completed':
        return { label: '完成', type: 'success' }
      case 'cancelled':
        return { label: '已取消', type: 'info' }
      case 'failed':
        return { label: '失败', type: 'danger' }
      default:
        return { label: '准备中', type: 'info' }
    }
  }
)

// 流水线当前步:诊断=0、检活=1、评分=2、完成=3(三步全 finish)。
// 终态(失败/取消)停在其语义所属步:诊断产出前=0,已进入检活/评分=1。
const activeStep = computed(() => {
  switch (phase.value) {
    case 'diagnosing':
      return 0
    case 'checking':
      return 1
    case 'scoring':
      return 2
    case 'completed':
      return 3
    case 'failed':
    case 'cancelled':
      return diagnosticReady.value ? 1 : 0
    default:
      return 0
  }
})

// 当前步的处理态色:运行态走主色(process),失败标红,取消标灰(wait)。
const stepProcessStatus = computed<'process' | 'error' | 'wait'>(() => {
  if (phase.value === 'failed') return 'error'
  if (phase.value === 'cancelled') return 'wait'
  return 'process'
})

// 检活步描述:进行中带 checked/total 绝对计数,其余阶段回退静态文案。
const checkStepDesc = computed(() => {
  if (phase.value === 'checking' && checkingProgress.value) {
    return `${checkingProgress.value.checked} / ${checkingProgress.value.total}`
  }
  return '逐节点连通性检测'
})

watch(visible, (val) => {
  if (!val) {
    stopPolling()
  }
})

// 成功 alert title 带口径(ticket 0044):实测完成(抽样,共检活 N 个节点)/全量同理;
// N 取 involvedCount(检活 cursor total,秒级运行由 run 维度兜底),极端缺失时只标模式。
// 失败/取消态同带模式口径(spec「口径一致」),与端点测试 alert 形态对齐。
const completedTitle = computed(() => {
  const mode = testFull.value ? '全量' : '抽样'
  return involvedCount.value !== null
    ? `实测完成（${mode}，共检活 ${involvedCount.value} 个节点）`
    : `实测完成（${mode}）`
})

const cancelledTitle = computed(() => {
  const mode = testFull.value ? '全量' : '抽样'
  return `测试已取消（${mode}）`
})

const failedTitle = computed(() => {
  const mode = testFull.value ? '全量' : '抽样'
  return `测试失败（${mode}）`
})

// 显式运行入口:父级(机场管理页/详情抽屉)在用户点「测试」/「重新测试」/「测全部」时调用。
const start = (target: Airport, full = false) => {
  visible.value = true
  startRun(target, full)
}

defineExpose({ start })

const handleClose = () => {
  reset()
}
</script>

<style scoped>
.dialog-head {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
}

.dialog-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}

.mode-line {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-3);
}

.mode-tag {
  flex: none;
}

.pipeline-steps {
  margin-bottom: var(--ph-space-4);
}

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

.diagnostic-alert {
  margin-bottom: var(--ph-space-5);
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

.drawer-hint {
  margin-top: var(--ph-space-2);
}

.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
