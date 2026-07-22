<template>
  <!-- 订阅测试段(ADR 0028):即时诊断不落库,显式动作才发请求。
       拉取验证 = 内部生成链校验(不发真实 HTTP、不计 pull_logs);
       现场实测 = 抽样检活(可升级全量),run 只存内存,轮询 404 提示重跑。 -->
  <div class="test-section">
    <div class="report-header">
      <div class="drawer-section-title">订阅测试</div>
      <div class="report-actions">
        <el-button size="small" type="primary" :loading="testLoading" @click="runTest">
          拉取验证
        </el-button>
        <el-button size="small" :disabled="probing" @click="startProbe(false)">现场实测</el-button>
        <el-button size="small" :disabled="probing" @click="startProbe(true)">测全部</el-button>
      </div>
    </div>

    <div v-if="!testResult" class="muted test-empty">
      尚未测试。「拉取验证」校验订阅内容合法性并给出池快照(不发真实请求、不计拉取统计);
      「现场实测」对下发节点抽样检活,「测全部」全量检活,结果写回节点池。
    </div>

    <template v-else>
      <!-- 拉取验证:双格式合法性/节点数/耗时 + 池快照 -->
      <el-descriptions :column="2" border size="small" class="report-block num">
        <el-descriptions-item label="Clash">
          <el-tag :type="testResult.pull.clash.valid ? 'success' : 'danger'" size="small">
            {{ testResult.pull.clash.valid ? '合法' : '异常' }}
          </el-tag>
          <span v-if="testResult.pull.clash.valid">
            {{ testResult.pull.clash.node_count }} 节点 · {{ testResult.pull.clash.duration_ms }}ms
          </span>
          <span v-else class="muted"> {{ testResult.pull.clash.error || '生成失败' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="V2Ray">
          <el-tag :type="testResult.pull.v2ray.valid ? 'success' : 'danger'" size="small">
            {{ testResult.pull.v2ray.valid ? '合法' : '异常' }}
          </el-tag>
          <span v-if="testResult.pull.v2ray.valid">
            {{ testResult.pull.v2ray.node_count }} 节点 · {{ testResult.pull.v2ray.duration_ms }}ms
          </span>
          <span v-else class="muted"> {{ testResult.pull.v2ray.error || '生成失败' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="池快照可用">
          {{ testResult.snapshot.available }} / {{ testResult.snapshot.total }}
        </el-descriptions-item>
        <el-descriptions-item label="平均延迟">{{ snapshotLatencyText }}</el-descriptions-item>
        <el-descriptions-item label="地区覆盖" :span="2">
          {{ testResult.snapshot.region_count }} 个地区
          <span v-if="testResult.snapshot.regions.length > 0" class="muted">
            ({{ testResult.snapshot.regions.join('/') }})
          </span>
        </el-descriptions-item>
      </el-descriptions>
    </template>

    <!-- 现场实测进度与结果(run 只存内存;完成后刷新拉取验证与池快照) -->
    <div v-if="probeRun" class="probe-block">
      <template v-if="probeRun.status === 'running'">
        <div class="probe-running muted">
          现场实测进行中({{ probeRun.full ? '全量' : '抽样' }})…
        </div>
        <el-progress
          :percentage="probePercent"
          :format="() => `${probeRun?.checked ?? 0} / ${probeRun ? probeTotalOf(probeRun) : 0}`"
        />
      </template>
      <el-alert
        v-else-if="probeRun.status === 'completed'"
        type="success"
        :closable="false"
        show-icon
        class="report-alert"
      >
        <template #title>
          实测完成({{ probeRun.full ? '全量' : '抽样' }},共检活 {{ probeRun.checked }} 个节点)
        </template>
        可用性结果已写回节点池,池快照已刷新。
      </el-alert>
      <el-alert v-else type="error" :closable="false" show-icon class="report-alert">
        <template #title>实测失败</template>
        {{ probeRun.error || '未知错误' }}
      </el-alert>
    </div>
    <el-alert v-if="probeLost" type="warning" :closable="false" show-icon class="report-alert">
      <template #title>实测进度已失效</template>
      实测记录只保存在服务内存,服务重启或过期后丢失,请重新发起实测。
    </el-alert>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import type { Endpoint } from '@/types'
import {
  runEndpointTest,
  startEndpointProbe,
  getEndpointProbe,
  probeTotalOf,
  isNotFound,
  type EndpointTestResult,
  type ProbeRun
} from '@/composables/useEndpointTest'

const props = defineProps<{ endpoint: Endpoint }>()

// 拉取验证 + 池快照:显式点击才发起(打开抽屉不自动测,见 CONTEXT.md「详情抽屉」)
const testResult = ref<EndpointTestResult | null>(null)
const testLoading = ref(false)

const runTest = async () => {
  testLoading.value = true
  try {
    testResult.value = await runEndpointTest(props.endpoint.id)
  } catch {
    // 拦截器已提示;保留旧结果,不清空
  } finally {
    testLoading.value = false
  }
}

const snapshotLatencyText = computed(() => {
  const snap = testResult.value?.snapshot
  return snap && snap.available > 0 ? `${snap.mean_latency_ms.toFixed(0)} ms` : '—'
})

// 现场实测:POST 拿 run 句柄后轮询(1.5s),completed 后刷新拉取验证与池快照;
// 轮询 404(run 重启丢失/过期)停止轮询并提示重跑(ADR 0028 决策 5)。
const PROBE_POLL_INTERVAL_MS = 1500
const probeRun = ref<ProbeRun | null>(null)
const probeLost = ref(false)
const probing = computed(() => probeRun.value?.status === 'running')
let pollTimer: number | null = null

const probePercent = computed(() => {
  const run = probeRun.value
  if (!run) return 0
  const total = probeTotalOf(run)
  return total > 0 ? Math.round((run.checked / total) * 100) : 0
})

const startProbe = async (full: boolean) => {
  stopProbePoll()
  probeRun.value = null
  probeLost.value = false
  try {
    const run = await startEndpointProbe(props.endpoint.id, full)
    probeRun.value = run
    if (run.status === 'running') {
      startProbePoll()
    } else if (run.status === 'completed') {
      onProbeCompleted()
    }
  } catch {
    // 拦截器已提示(如无可下发节点 400);保持无 run 态
    probeRun.value = null
  }
}

const startProbePoll = () => {
  stopProbePoll()
  pollTimer = window.setInterval(pollProbe, PROBE_POLL_INTERVAL_MS)
}

const stopProbePoll = () => {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

const pollProbe = async () => {
  const run = probeRun.value
  if (!run) return
  try {
    const latest = await getEndpointProbe(props.endpoint.id, run.run_id)
    probeRun.value = latest
    if (latest.status === 'completed') {
      stopProbePoll()
      onProbeCompleted()
    } else if (latest.status === 'failed') {
      stopProbePoll()
    }
  } catch (err) {
    if (isNotFound(err)) {
      // run 只存内存:重启/过期后 404,停止轮询提示重跑
      stopProbePoll()
      probeRun.value = null
      probeLost.value = true
    }
    // 其他瞬时错误:静默,下一轮继续
  }
}

// 实测写回了池可用性:刷新拉取验证与池快照(同段数据,同一接口)
const onProbeCompleted = () => {
  runTest()
}

// 抽屉关闭(父级 v-if 卸载本段)/切换端点时停止轮询并清空,避免闪现旧端点数据
watch(
  () => props.endpoint.id,
  () => {
    stopProbePoll()
    testResult.value = null
    probeRun.value = null
    probeLost.value = false
  }
)

onUnmounted(stopProbePoll)
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
.report-block {
  margin-bottom: var(--ph-space-3);
}
.report-alert {
  margin-bottom: var(--ph-space-3);
}
.test-empty {
  padding: var(--ph-space-2) 0 var(--ph-space-3);
  line-height: 1.6;
}
.probe-block {
  margin-top: var(--ph-space-3);
}
.probe-running {
  margin-bottom: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
