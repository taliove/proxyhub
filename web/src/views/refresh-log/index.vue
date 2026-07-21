<template>
  <div>
    <!-- 实时进度:仅在有活跃刷新任务时出现的状态横幅,非详情容器 -->
    <el-card v-if="activeRun" class="active-card">
      <template #header>
        <div class="card-header">
          <span>
            <el-tag :type="statusTagType(activeRun.status)" size="small">{{
              statusLabel(activeRun.status)
            }}</el-tag>
            刷新中…({{ activeEvents.length }} 条事件)
          </span>
          <el-button
            v-if="activeRun.status !== 'running'"
            link
            type="primary"
            @click="activeRun = null"
            >收起</el-button
          >
        </div>
      </template>
      <el-alert v-if="pollError" class="stack-alert" type="warning" :closable="false" show-icon>
        轮询中断:{{ pollError }},请稍后手动刷新列表
      </el-alert>
      <RefreshEventList :events="activeEvents" />
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>刷新记录</span>
          <el-button type="success" :loading="refreshing" @click="refreshNow">
            <el-icon><Refresh /></el-icon> 立即刷新
          </el-button>
        </div>
      </template>

      <el-alert v-if="loadError" class="stack-alert" type="error" :closable="false" show-icon>
        加载失败:{{ loadError }}
        <el-button link type="primary" @click="loadRuns">重试</el-button>
      </el-alert>

      <!-- 详情进右侧抽屉(见 design-frontend.md 详情模式),不用 expand 展开行 -->
      <el-table v-loading="loading" :data="runs" row-key="id" @row-click="openDetail">
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
        </el-table-column>
        <el-table-column label="触发" width="100">
          <template #default="{ row }">{{ triggerLabel(row.trigger) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="节点数" width="120">
          <template #default="{ row }">{{ formatNodes(row) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <RefreshDetailDrawer
      v-model="detailVisible"
      :run="detailRun"
      :events="detailRun ? eventCache[detailRun.id] : undefined"
      :error="detailRun ? eventError[detailRun.id] : undefined"
      :open-data="openData"
      @toggle-data="toggleData"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import type { RefreshRun, RefreshEvent } from '@/types'
import client from '@/api/client'
import RefreshEventList from './components/RefreshEventList.vue'
import RefreshDetailDrawer from './components/RefreshDetailDrawer.vue'
import {
  errMessage,
  isConflict,
  triggerLabel,
  statusTagType,
  statusLabel,
  formatNodes,
  formatTime,
  dedupeEvents
} from './utils'

const POLL_INTERVAL = 2000
const MAX_POLL_RETRIES = 3

const runs = ref<RefreshRun[]>([])
const loading = ref(false)
const refreshing = ref(false)
const loadError = ref('')
const eventCache = reactive<Record<number, RefreshEvent[]>>({})
const eventError = reactive<Record<number, string>>({})
const openData = reactive<Record<number, boolean>>({})

// 详情抽屉
const detailVisible = ref(false)
const detailRun = ref<RefreshRun | null>(null)

// 实时进度
const activeRun = ref<RefreshRun | null>(null)
const activeEvents = ref<RefreshEvent[]>([])
const pollError = ref('')
let pollTimer: ReturnType<typeof setTimeout> | null = null
let pollRetries = 0
let pollGen = 0

const loadRuns = async () => {
  loading.value = true
  loadError.value = ''
  try {
    runs.value = await client.get('/refresh/runs')
  } catch (e) {
    loadError.value = errMessage(e)
  } finally {
    loading.value = false
  }
}

const loadEvents = async (runId: number) => {
  try {
    const resp = (await client.get(`/refresh/runs/${runId}`)) as {
      run: RefreshRun
      events: RefreshEvent[]
    }
    eventCache[runId] = resp.events || []
    delete eventError[runId]
  } catch (e) {
    eventError[runId] = errMessage(e)
  }
}

// 打开详情抽屉:懒加载该 run 的事件
const openDetail = (row: RefreshRun) => {
  detailRun.value = row
  detailVisible.value = true
  if (!eventCache[row.id] && !eventError[row.id]) {
    loadEvents(row.id)
  }
}

const toggleData = (eventId: number) => {
  openData[eventId] = !openData[eventId]
}
// 刷新已任务化(ticket 03):POST 返回 jobId,refresh_runs 记录异步创建,
// 需按 job_id 反查 run 再轮询;ticket 05 将整体迁入任务中心。
const refreshNow = async () => {
  refreshing.value = true
  try {
    const resp = (await client.post('/aggregator/refresh')) as { ok: boolean; jobId: number }
    const runId = await waitRunByJobId(resp.jobId)
    if (runId !== null) startPolling(runId)
  } catch (e) {
    if (isConflict(e)) {
      ElMessage.warning('已有刷新任务在进行中，已定位到该记录')
      const running = runs.value.find((r) => r.status === 'running')
      if (running) startPolling(running.id)
    }
  } finally {
    refreshing.value = false
  }
}

// waitRunByJobId 轮询刷新记录列表,找到关联该任务的 run 后返回其 id(超时返回 null)。
const waitRunByJobId = async (jobId: number): Promise<number | null> => {
  const deadline = Date.now() + 10000
  while (Date.now() < deadline) {
    await loadRuns()
    const run = runs.value.find((r) => r.job_id === jobId)
    if (run) return run.id
    await new Promise((resolve) => setTimeout(resolve, 300))
  }
  return null
}

const startPolling = (runId: number) => {
  stopPolling()
  pollGen++
  const gen = pollGen
  pollError.value = ''
  pollRetries = 0
  activeRun.value = {
    id: runId,
    status: 'running',
    trigger: 'manual',
    job_id: 0,
    total_nodes: 0,
    available_nodes: 0,
    final_nodes: 0,
    error: '',
    started_at: new Date().toISOString(),
    finished_at: null
  }
  activeEvents.value = []
  pollOnce(runId, gen)
}

const stopPolling = () => {
  pollGen++
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

const pollOnce = async (runId: number, gen: number) => {
  try {
    const resp = (await client.get(`/refresh/runs/${runId}`)) as {
      run: RefreshRun
      events: RefreshEvent[]
    }
    if (gen !== pollGen) return
    activeRun.value = resp.run
    activeEvents.value = dedupeEvents(activeEvents.value, resp.events || [])
    pollRetries = 0
    if (resp.run.status !== 'running') {
      finishActiveRun(resp.run)
      return
    }
    pollTimer = setTimeout(() => pollOnce(runId, gen), POLL_INTERVAL)
  } catch (e) {
    if (gen !== pollGen) return
    pollRetries++
    if (pollRetries > MAX_POLL_RETRIES) {
      pollError.value = errMessage(e)
      stopPolling()
      return
    }
    pollTimer = setTimeout(() => pollOnce(runId, gen), POLL_INTERVAL)
  }
}

const finishActiveRun = (run: RefreshRun) => {
  stopPolling()
  // 列表里可能已有这条 run（手动刷新时会先创建再轮询），替换；否则插顶部
  const idx = runs.value.findIndex((r) => r.id === run.id)
  if (idx >= 0) {
    runs.value = runs.value.map((r) => (r.id === run.id ? run : r))
  } else {
    runs.value = [run, ...runs.value]
  }
  // 清缓存让打开详情时重新拉取完整事件
  delete eventCache[run.id]
  activeRun.value = null
  activeEvents.value = []
}

onMounted(loadRuns)
onUnmounted(stopPolling)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.active-card {
  margin-bottom: var(--ph-space-4);
}
.stack-alert {
  margin-bottom: var(--ph-space-3);
}
</style>
