<template>
  <el-dialog
    v-model="visible"
    :title="`机场测试 - ${airport?.name || ''}`"
    width="700px"
    @close="handleClose"
  >
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

    <!-- Completed phase: 纯运行形态只给结论与去向,报告主体在详情抽屉「最近测试」段 -->
    <div v-else-if="phase === 'completed'" class="test-phase">
      <div class="completed-summary">
        <div class="completed-score">
          <StatusDot :tone="scoreToneOf(overallScore)" :label="scoreToneLabelOf(overallScore)" />
          <span>测试完成,综合得分</span>
          <el-tag :type="getScoreColor(overallScore)" size="large" class="score-value num">
            {{ overallScore.toFixed(1) }}
          </el-tag>
        </div>
        <div class="muted">报告已更新,可在机场详情抽屉「最近测试」查看完整报告。</div>
      </div>
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
import { ref, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import type { Airport } from '@/types'
import { useAirportTestRun } from '@/composables/useAirportTestRun'
import { getScoreColor } from '@/composables/useAirportTest'
import {
  scoreTone as scoreToneOf,
  scoreToneLabel as scoreToneLabelOf
} from '@/views/airport-test-utils'
import StatusDot from '@/components/StatusDot.vue'
import AirportTestDiagnostic from './AirportTestDiagnostic.vue'

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
  errorMessage,
  isRunningPhase,
  involvedCount,
  start: startRun,
  cancel: onCancel,
  reset,
  stopPolling
} = useAirportTestRun(() => emit('finished'))

watch(visible, (val) => {
  if (!val) {
    stopPolling()
  }
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
.mode-line {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-3);
}

.mode-tag {
  flex: none;
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

.completed-summary {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-5) 0;
}

.completed-score {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  font-size: var(--ph-text-md);
}

.score-value {
  font-size: var(--ph-text-xl);
  font-weight: bold;
}

.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
