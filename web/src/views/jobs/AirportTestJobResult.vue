<template>
  <div>
    <div v-if="loading" class="muted">加载测试报告…</div>
    <div v-else-if="result?.reason === 'no_report'" class="muted">
      本次任务未产生报告（可能已被中断）
    </div>
    <template v-else-if="run">
      <!-- completed:复用详情抽屉同款报告组件(只读,无重跑入口),
           报告职责自 ScoreReport 收敛到 AirportTestReport 后此处同步切换 -->
      <AirportTestReport v-if="run.status === 'completed'" :runs="[run]" readonly />
      <template v-else>
        <el-alert
          v-if="run.status === 'cancelled'"
          type="info"
          :closable="false"
          show-icon
          class="test-alert"
        >
          <template #title>本次测试已取消</template>
          已写回的节点检活结果保留，未产生评分报告。
        </el-alert>
        <el-alert
          v-else-if="run.status === 'failed'"
          type="error"
          :closable="false"
          show-icon
          class="test-alert"
        >
          <template #title>测试失败</template>
          {{ run.error_message || '未知错误' }}
        </el-alert>
        <div v-else class="muted">测试进行中，报告生成后展示</div>
        <template v-if="hasDiagnostic && run.status !== 'diagnosing'">
          <h4 class="test-diag-title">📊 诊断结果</h4>
          <AirportTestDiagnostic :diagnostic="diagnostic" />
        </template>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { getJobResult, type JobResult } from '@/api/jobs'
import { emptyDiagnostic, parseDiagnosticResult, type TestRun } from '@/composables/useAirportTest'
import AirportTestReport from '@/components/AirportTestReport.vue'
import AirportTestDiagnostic from '@/components/AirportTestDiagnostic.vue'

// 任务详情的机场测试报告区(ticket 0026):消费 GET /api/jobs/{id}/result 的
// airport_test_run 分支(与 0023 exam 结果区同款机制)。completed 复用
// AirportTestReport(只读,无重跑入口);cancelled 保留诊断数据;
// failed 显示 run 行 error_message。
const props = defineProps<{
  jobId: number
}>()

const result = ref<JobResult | null>(null)
const loading = ref(false)

const run = computed<TestRun | null>(() => result.value?.airport_test_run ?? null)
const diagnostic = computed(() =>
  run.value ? parseDiagnosticResult(run.value.dimensions_json) : emptyDiagnostic()
)
const hasDiagnostic = computed(
  () => diagnostic.value.http_status > 0 || diagnostic.value.node_count > 0
)

const load = async (jobId: number) => {
  loading.value = true
  result.value = null
  try {
    result.value = await getJobResult(jobId)
  } catch {
    // 结果加载失败不阻塞弹框主信息(全局拦截器已提示)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.jobId,
  (jobId) => {
    if (jobId > 0) load(jobId)
  },
  { immediate: true }
)
</script>

<style scoped>
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.test-alert {
  margin-bottom: var(--ph-space-3);
}
.test-diag-title {
  margin: 0 0 var(--ph-space-3);
}
</style>
