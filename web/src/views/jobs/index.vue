<template>
  <div>
    <ScheduleCard />

    <el-card>
      <template #header>
        <div class="card-header">
          <span>任务中心</span>
          <div class="header-actions">
            <span v-if="polling" class="muted">运行中,自动刷新</span>
            <el-button :loading="loading" @click="reload">刷新</el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="jobs">
        <el-table-column label="任务" min-width="140">
          <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
        </el-table-column>
        <el-table-column prop="key" label="标识" min-width="120" show-overflow-tooltip />
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="statusMeta(row.status).tag" size="small">{{
              statusMeta(row.status).label
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="进度" width="120">
          <template #default="{ row }">{{ parseProgress(row.cursor) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">{{ row.created_at }}</template>
        </el-table-column>
        <el-table-column label="更新时间" width="180">
          <template #default="{ row }">{{ row.updated_at }}</template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button v-if="isRunning(row.status)" link type="warning" @click="onCancel(row)"
              >取消</el-button
            >
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && jobs.length === 0" description="暂无任务记录" :image-size="60" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { listJobs, cancelJob, type Job } from '@/api/jobs'
import { kindLabel, statusMeta, isRunning, parseProgress } from './jobmeta'
import ScheduleCard from './ScheduleCard.vue'

const jobs = ref<Job[]>([])
const loading = ref(false)
const polling = ref(false)
let pollTimer: number | null = null

const POLL_INTERVAL_MS = 4000

const stopPolling = () => {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  polling.value = false
}

// 有运行中任务则开轮询,否则停轮询(幂等)。
const syncPolling = () => {
  const hasRunning = jobs.value.some((j) => isRunning(j.status))
  if (hasRunning && pollTimer === null) {
    polling.value = true
    pollTimer = window.setInterval(fetchJobs, POLL_INTERVAL_MS)
  } else if (!hasRunning) {
    stopPolling()
  }
}

const fetchJobs = async () => {
  try {
    jobs.value = await listJobs()
    syncPolling()
  } catch {
    stopPolling()
  }
}

const reload = async () => {
  loading.value = true
  try {
    await fetchJobs()
  } finally {
    loading.value = false
  }
}

const onCancel = async (row: Job) => {
  try {
    await cancelJob(row.kind, row.key)
    ElMessage.success('已发送取消')
    await fetchJobs()
  } catch {
    // 409 = 无活动任务,全局拦截器静默;此处刷新纠正视图
    await fetchJobs()
  }
}

onMounted(reload)
onUnmounted(stopPolling)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
