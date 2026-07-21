<template>
  <el-dialog v-model="visible" title="任务详情" width="560px">
    <template v-if="job">
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item label="任务类型">{{ kindLabel(job.kind) }}</el-descriptions-item>
        <el-descriptions-item label="范围">{{ scopeLabel(job) }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusMeta(job.status).tag" size="small">
            {{ statusMeta(job.status).label }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="进度">
          {{ progressText }}
          <span v-if="isRunning(job.status)" class="muted">(已完成数)</span>
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ job.created_at }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ job.updated_at }}</el-descriptions-item>
        <el-descriptions-item label="原始标识" :span="2">
          <span class="mono">{{ job.kind }}/{{ job.key }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <!-- 刷新任务:关联 refresh_runs 的统计与每机场事件流 -->
      <template v-if="job.kind === 'refresh'">
        <el-divider content-position="left">刷新详情</el-divider>
        <div v-if="refreshLoading" class="muted">加载刷新记录…</div>
        <template v-else-if="refreshRun">
          <el-descriptions :column="3" border size="small" class="refresh-stats">
            <el-descriptions-item label="拉取节点">{{
              refreshRun.total_nodes
            }}</el-descriptions-item>
            <el-descriptions-item label="可用">{{
              refreshRun.available_nodes
            }}</el-descriptions-item>
            <el-descriptions-item label="最终入池">{{
              refreshRun.final_nodes
            }}</el-descriptions-item>
            <el-descriptions-item label="开始">{{ refreshRun.started_at }}</el-descriptions-item>
            <el-descriptions-item label="结束" :span="2">
              {{ refreshRun.finished_at || '-' }}
            </el-descriptions-item>
            <el-descriptions-item v-if="refreshRun.error" label="备注" :span="3">
              {{ refreshRun.error }}
            </el-descriptions-item>
          </el-descriptions>
          <el-timeline v-if="refreshEvents.length" class="refresh-events">
            <el-timeline-item
              v-for="ev in refreshEvents"
              :key="ev.id"
              :type="eventTagType(ev.level)"
              :timestamp="formatEventTime(ev.created_at)"
              size="small"
            >
              {{ ev.message }}
            </el-timeline-item>
          </el-timeline>
        </template>
        <div v-else class="muted">暂无关联刷新记录(任务刚启动或记录已滚动清理)</div>
      </template>

      <!-- 参数:key 列表可能数百个,默认折叠 -->
      <el-collapse v-if="paramsText" class="params-block">
        <el-collapse-item :title="`启动参数${nodeCount !== null ? `(节点数 ${nodeCount})` : ''}`">
          <pre class="params-pre">{{ paramsText }}</pre>
        </el-collapse-item>
      </el-collapse>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Job } from '@/api/jobs'
import { findRefreshRunByJob, getRefreshRun } from '@/api/refresh'
import type { RefreshRun, RefreshEvent } from '@/types'
import {
  kindLabel,
  statusMeta,
  isRunning,
  parseProgress,
  parseJobParams,
  scopeLabel
} from './jobmeta'

// 任务详情弹框:展示 jobs 表全字段 + 启动参数(折叠);
// kind=refresh 时追加关联 refresh_runs 的统计与事件流(按 job_id 反查)。
const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  job: Job | null
}>()

const parsed = computed(() => (props.job ? parseJobParams(props.job.params) : null))
const nodeCount = computed(() => parsed.value?.node_keys?.length ?? null)
const progressText = computed(() => (props.job ? parseProgress(props.job.cursor) : '-'))
const paramsText = computed(() => {
  if (!props.job?.params) return ''
  try {
    return JSON.stringify(JSON.parse(props.job.params), null, 2)
  } catch {
    return props.job.params
  }
})

// 刷新详情:run 记录异步创建,反查失败时短重试几次
const refreshRun = ref<RefreshRun | null>(null)
const refreshEvents = ref<RefreshEvent[]>([])
const refreshLoading = ref(false)

const loadRefreshDetail = async (jobId: number) => {
  refreshLoading.value = true
  refreshRun.value = null
  refreshEvents.value = []
  try {
    let run: RefreshRun | null = null
    for (let i = 0; i < 5 && !run; i++) {
      run = await findRefreshRunByJob(jobId)
      if (!run) await new Promise((resolve) => setTimeout(resolve, 400))
    }
    if (run) {
      refreshRun.value = run
      const detail = await getRefreshRun(run.id)
      refreshEvents.value = detail.events || []
    }
  } catch {
    // 详情加载失败不阻塞弹框主信息
  } finally {
    refreshLoading.value = false
  }
}

watch(
  () => [props.job?.id, visible.value] as const,
  ([jobId, vis]) => {
    if (vis && jobId && props.job?.kind === 'refresh') {
      loadRefreshDetail(jobId)
    }
  },
  { immediate: true }
)

const eventTagType = (level: string): 'primary' | 'success' | 'warning' | 'danger' | 'info' => {
  if (level === 'error') return 'danger'
  if (level === 'warn') return 'warning'
  return 'info'
}

const formatEventTime = (ts: string) => (ts.length > 19 ? ts.slice(11, 19) : ts)
</script>

<style scoped>
.params-block {
  margin-top: var(--ph-space-4);
}
.params-pre {
  margin: 0;
  max-height: 300px;
  overflow: auto;
  font-size: var(--ph-text-xs);
  white-space: pre-wrap;
  word-break: break-all;
}
.mono {
  font-family: monospace;
  font-size: var(--ph-text-xs);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.refresh-stats {
  margin-bottom: var(--ph-space-3);
}
.refresh-events {
  max-height: 320px;
  overflow: auto;
  padding-left: 2px;
}
</style>
