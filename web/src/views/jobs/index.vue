<template>
  <div>
    <PageHeader>
      <span v-if="polling" class="muted">运行中,自动刷新</span>
      <el-select v-model="sourceFilter" class="source-filter" size="small">
        <el-option label="手动发起" value="手动" />
        <el-option label="定时" value="定时" />
        <el-option label="启动" value="启动" />
        <el-option label="全部来源" value="" />
      </el-select>
      <el-button :loading="loading" @click="reload">刷新</el-button>
    </PageHeader>

    <ScheduleCard />

    <el-card>
      <el-table v-loading="loading" :data="filteredJobs" @row-click="openDetail">
        <el-table-column label="任务" min-width="140">
          <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
        </el-table-column>
        <el-table-column label="标识" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ scopeLabel(row) }}</template>
        </el-table-column>
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
            <el-button v-if="isRunning(row.status)" link type="warning" @click.stop="onCancel(row)"
              >取消</el-button
            >
          </template>
        </el-table-column>
      </el-table>

      <el-empty
        v-if="!loading && filteredJobs.length === 0"
        description="暂无任务记录"
        :image-size="60"
      />
    </el-card>

    <JobDetailDialog v-model="detailVisible" :job="detailJob" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { listJobs, cancelJob, type Job } from '@/api/jobs'
import { kindLabel, statusMeta, isRunning, parseProgress, scopeLabel, jobTrigger } from './jobmeta'
import PageHeader from '@/components/PageHeader.vue'
import ScheduleCard from './ScheduleCard.vue'
import JobDetailDialog from './JobDetailDialog.vue'

const jobs = ref<Job[]>([])
const loading = ref(false)

// 来源筛选:默认只显示手动发起的任务(定时/启动的刷新任务不刷屏),可切换查看
const sourceFilter = ref('手动')
const filteredJobs = computed(() =>
  sourceFilter.value === ''
    ? jobs.value
    : jobs.value.filter((j) => jobTrigger(j) === sourceFilter.value)
)
const polling = ref(false)
let pollTimer: number | null = null

// 详情弹框:点击行打开,数据直接取列表行(已含 params)
const detailVisible = ref(false)
const detailJob = ref<Job | null>(null)
const openDetail = (row: Job) => {
  detailJob.value = row
  detailVisible.value = true
}

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
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
:deep(.el-table__row) {
  cursor: pointer;
}
.source-filter {
  width: 110px;
}
</style>
