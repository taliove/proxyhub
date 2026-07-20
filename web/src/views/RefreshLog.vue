<template>
  <div>
    <el-card v-if="activeRun" style="margin-bottom: 16px">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span>
            <el-tag :type="statusTagType(activeRun.status)" size="small">{{
              statusLabel(activeRun.status)
            }}</el-tag>
            刷新中…（{{ activeEvents.length }} 条事件）
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
      <el-alert
        v-if="pollError"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 8px"
      >
        轮询中断：{{ pollError }}，请稍后手动刷新列表
      </el-alert>
      <div>
        <div
          v-for="ev in activeEvents"
          :key="ev.id"
          style="display: flex; gap: 8px; padding: 4px 0; align-items: flex-start"
        >
          <el-icon style="margin-top: 2px"><component :is="levelIcon(ev.level)" /></el-icon>
          <span style="color: #909399; font-size: 12px; min-width: 70px">{{
            formatTime(ev.created_at)
          }}</span>
          <span style="flex: 1">{{ ev.message }}</span>
        </div>
      </div>
    </el-card>

    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between">
          <span>刷新记录</span>
          <el-button type="success" :loading="refreshing" @click="refreshNow">
            <el-icon><Refresh /></el-icon> 立即刷新
          </el-button>
        </div>
      </template>

      <el-alert
        v-if="loadError"
        style="margin-bottom: 12px"
        type="error"
        :closable="false"
        show-icon
      >
        加载失败：{{ loadError }}
        <el-button link type="primary" @click="loadRuns">重试</el-button>
      </el-alert>

      <el-table v-loading="loading" :data="runs" row-key="id" @expand-change="onExpand">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div style="padding: 12px 24px">
              <el-alert v-if="eventError[row.id]" type="error" :closable="false" show-icon>
                事件加载失败：{{ eventError[row.id] }}
              </el-alert>
              <el-skeleton v-else-if="!eventCache[row.id]" :rows="3" animated />
              <div v-else>
                <div
                  v-for="group in groupedEvents(row.id)"
                  :key="group.stage"
                  style="margin-bottom: 16px"
                >
                  <div style="font-weight: 600; margin-bottom: 8px; color: #606266">
                    {{ stageLabel(group.stage) }}
                  </div>
                  <div
                    v-for="ev in group.events"
                    :key="ev.id"
                    style="display: flex; gap: 8px; padding: 4px 0; align-items: flex-start"
                  >
                    <el-icon style="margin-top: 2px"
                      ><component :is="levelIcon(ev.level)"
                    /></el-icon>
                    <span style="color: #909399; font-size: 12px; min-width: 70px">{{
                      formatTime(ev.created_at)
                    }}</span>
                    <span style="flex: 1">{{ ev.message }}</span>
                    <el-button v-if="ev.data" link type="info" @click="toggleData(ev.id)">
                      {{ openData[ev.id] ? '收起' : '数据' }}
                    </el-button>
                  </div>
                  <pre
                    v-if="openDataShow(group.events)"
                    style="
                      background: #f5f7fa;
                      padding: 8px;
                      margin: 4px 0 0 84px;
                      font-size: 12px;
                      overflow-x: auto;
                    "
                    >{{ prettyData(group.events) }}</pre>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>
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
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, InfoFilled, WarningFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import type { RefreshRun, RefreshEvent } from '@/types'
import client from '@/api/client'

const POLL_INTERVAL = 2000
const MAX_POLL_RETRIES = 3

const runs = ref<RefreshRun[]>([])
const loading = ref(false)
const refreshing = ref(false)
const loadError = ref('')
const eventCache = reactive<Record<number, RefreshEvent[]>>({})
const eventError = reactive<Record<number, string>>({})
const openData = reactive<Record<number, boolean>>({})

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
  } catch (e: any) {
    loadError.value = e?.response?.data || e?.message || '未知错误'
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
  } catch (e: any) {
    eventError[runId] = e?.response?.data || e?.message || '未知错误'
  }
}

const onExpand = (row: RefreshRun, expanded: RefreshRun[]) => {
  const isOpen = expanded.some((r) => r.id === row.id)
  if (isOpen && !eventCache[row.id] && !eventError[row.id]) {
    loadEvents(row.id)
  }
}

const toggleData = (eventId: number) => {
  openData[eventId] = !openData[eventId]
}

const openDataShow = (events: RefreshEvent[]): boolean =>
  events.some((e) => e.data && openData[e.id])

const prettyData = (events: RefreshEvent[]): string => {
  const lines: string[] = []
  for (const e of events) {
    if (e.data && openData[e.id]) {
      try {
        lines.push(`${formatTime(e.created_at)}: ${JSON.stringify(JSON.parse(e.data), null, 2)}`)
      } catch {
        lines.push(`${formatTime(e.created_at)}: ${e.data}`)
      }
    }
  }
  return lines.join('\n')
}

interface EventGroup {
  stage: string
  events: RefreshEvent[]
}

const groupedEvents = (runId: number): EventGroup[] => {
  const events = eventCache[runId] || []
  const map = new Map<string, RefreshEvent[]>()
  for (const e of events) {
    if (!map.has(e.stage)) map.set(e.stage, [])
    map.get(e.stage)!.push(e)
  }
  const order = ['fetch', 'check', 'filter', 'done']
  return order.filter((s) => map.has(s)).map((s) => ({ stage: s, events: map.get(s)! }))
}

// 按 event id 去重，返回新数组（不可变）
const dedupeEvents = (existing: RefreshEvent[], incoming: RefreshEvent[]): RefreshEvent[] => {
  const seen = new Set(existing.map((e) => e.id))
  const fresh = incoming.filter((e) => !seen.has(e.id))
  return [...existing, ...fresh]
}

const refreshNow = async () => {
  refreshing.value = true
  try {
    const resp = (await client.post('/aggregator/refresh')) as { ok: boolean; runId: number }
    startPolling(resp.runId)
  } catch (e: any) {
    if (e?.response?.status === 409) {
      ElMessage.warning('已有刷新任务在进行中，已定位到该记录')
      const running = runs.value.find((r) => r.status === 'running')
      if (running) startPolling(running.id)
    }
  } finally {
    refreshing.value = false
  }
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
  } catch (e: any) {
    if (gen !== pollGen) return
    pollRetries++
    if (pollRetries > MAX_POLL_RETRIES) {
      pollError.value = e?.response?.data || e?.message || '未知错误'
      stopPolling()
      return
    }
    pollTimer = setTimeout(() => pollOnce(runId, gen), POLL_INTERVAL)
  }
}

const finishActiveRun = async (run: RefreshRun) => {
  stopPolling()
  // 列表里可能已有这条 run（手动刷新时会先创建再轮询），替换；否则插顶部
  const idx = runs.value.findIndex((r) => r.id === run.id)
  if (idx >= 0) {
    runs.value = runs.value.map((r) => (r.id === run.id ? run : r))
  } else {
    runs.value = [run, ...runs.value]
  }
  // 清缓存让展开时重新拉取完整事件
  delete eventCache[run.id]
  activeRun.value = null
  activeEvents.value = []
}

const triggerLabel = (t: string): string =>
  (({ manual: '手动', scheduled: '定时', startup: '启动' }) as Record<string, string>)[t] || t

const statusTagType = (s: string): 'success' | 'warning' | 'danger' | 'primary' | 'info' =>
  (({ success: 'success', partial: 'warning', failed: 'danger', running: 'primary' }) as const)[
    s as 'success'
  ] || 'info'

const statusLabel = (s: string): string =>
  (
    ({ success: '成功', partial: '部分', failed: '失败', running: '进行中' }) as Record<
      string,
      string
    >
  )[s] || s

const stageLabel = (s: string): string =>
  (({ fetch: '拉取', check: '健康检查', filter: '过滤', done: '完成' }) as Record<string, string>)[
    s
  ] || s

const levelIcon = (l: string) =>
  (({ info: InfoFilled, warn: WarningFilled, error: CircleCloseFilled }) as Record<string, any>)[
    l
  ] || InfoFilled

const formatNodes = (row: RefreshRun): string => {
  if (row.status === 'failed' && row.total_nodes === 0) return '—'
  return `${row.total_nodes}/${row.available_nodes}/${row.final_nodes}`
}

const formatTime = (iso: string): string => {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

onMounted(loadRuns)
onUnmounted(stopPolling)
</script>
