<template>
  <el-drawer v-model="visible" title="任务详情" size="640px">
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
        <el-descriptions-item label="创建时间">{{
          formatTime(job.created_at)
        }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{
          formatTime(job.updated_at)
        }}</el-descriptions-item>
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
            <el-descriptions-item label="开始">{{
              formatTime(refreshRun.started_at)
            }}</el-descriptions-item>
            <el-descriptions-item label="结束" :span="2">
              {{ refreshRun.finished_at ? formatTime(refreshRun.finished_at) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item v-if="refreshRun.error" label="备注" :span="3">
              {{ refreshRun.error }}
            </el-descriptions-item>
          </el-descriptions>
          <el-table
            v-if="refreshDiags.length"
            :data="refreshDiags"
            size="small"
            border
            class="refresh-diags"
          >
            <el-table-column label="机场" min-width="110">
              <template #default="{ row }">
                <span>{{ row.airport }}</span>
                <el-tooltip v-if="row.error" :content="row.error" placement="top">
                  <el-tag type="danger" size="small" class="diag-fail-tag">失败</el-tag>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="HTTP" width="70" align="right">
              <template #default="{ row }">{{ row.http_status || '-' }}</template>
            </el-table-column>
            <el-table-column label="耗时" width="90" align="right">
              <template #default="{ row }">{{ row.duration_ms }}ms</template>
            </el-table-column>
            <el-table-column prop="node_count" label="解析成功" width="80" align="right" />
            <el-table-column label="解析失败行" width="90" align="right">
              <template #default="{ row }">
                <el-tag v-if="row.parse_failures > 0" type="warning" size="small">
                  {{ row.parse_failures }}
                </el-tag>
                <span v-else>0</span>
              </template>
            </el-table-column>
          </el-table>
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
        <div v-else class="muted">暂无关联刷新记录（任务刚启动或记录已滚动清理）</div>
      </template>

      <!-- 体检任务(exam/batch_exam):消费 GET /api/jobs/{id}/result 的报告(ticket 0023)。
           exam 单份直出;batch_exam 按节点列表逐份展开(spec 遗留待决 1 定夺)。
           报告卡复用 ExamReportCard(内置 ExamShareDialog 分享入口)。 -->
      <template v-if="isExamKind">
        <el-divider content-position="left">体检结果</el-divider>
        <div v-if="examLoading" class="muted">加载体检报告…</div>
        <div v-else-if="examResult?.reason === 'no_report'" class="muted">
          本次任务未产生报告（可能已被中断）
        </div>
        <div v-else-if="reportRows.length === 0" class="muted">暂无体检报告</div>
        <template v-else>
          <template v-if="job.kind === 'exam'">
            <el-alert
              v-if="reportRows[0].fallback"
              type="warning"
              :closable="false"
              class="fallback-alert"
              title="非本次任务产出：该报告为任务时间窗内匹配的历史体检记录"
            />
            <ExamReportCard
              v-if="reportRows[0].report"
              :report="reportRows[0].report"
              :node-name="reportRows[0].nodeKey"
              :exam-time="reportRows[0].createdAt"
            />
          </template>
          <ul v-else class="batch-report-list">
            <li v-for="row in reportRows" :key="row.nodeKey" class="batch-report-item">
              <button type="button" class="batch-report-row" @click="toggleReport(row.nodeKey)">
                <span class="mono batch-report-key">{{ row.nodeKey }}</span>
                <el-tag v-if="row.fallback" type="warning" size="small">非本次任务产出</el-tag>
                <el-tag v-if="row.score !== null" :type="scoreTagType(row.score)" size="small">
                  稳定性 {{ row.score }}
                </el-tag>
                <el-tag v-else size="small" type="info">稳定性 —</el-tag>
                <span class="muted batch-report-time">{{ row.createdAt }}</span>
                <el-icon
                  class="batch-report-caret"
                  :class="{ 'is-open': expandedReports.includes(row.nodeKey) }"
                >
                  <ArrowRight />
                </el-icon>
              </button>
              <ExamReportCard
                v-if="expandedReports.includes(row.nodeKey) && row.report"
                :report="row.report"
                :node-name="row.nodeKey"
                :exam-time="row.createdAt"
                class="batch-report-card"
              />
            </li>
          </ul>
        </template>
      </template>

      <!-- 机场测试任务(airport_test):报告区抽为 AirportTestJobResult(ticket 0026,
           与 0023 exam 结果区同款 getJobResult 机制;cancelled 有对应展示) -->
      <template v-if="job.kind === 'airport_test'">
        <el-divider content-position="left">机场测试报告</el-divider>
        <AirportTestJobResult :job-id="job.id" />
      </template>

      <!-- 参数:key 列表可能数百个,默认折叠 -->
      <el-collapse v-if="paramsText" class="params-block">
        <el-collapse-item :title="`启动参数${nodeCount !== null ? `(节点数 ${nodeCount})` : ''}`">
          <pre class="params-pre">{{ paramsText }}</pre>
        </el-collapse-item>
      </el-collapse>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowRight } from '@element-plus/icons-vue'
import type { Job, JobResult, ExamJobReport } from '@/api/jobs'
import { getJobResult } from '@/api/jobs'
import { findRefreshRunByJob, getRefreshRun } from '@/api/refresh'
import type { RefreshRun, RefreshEvent, RefreshFetchDiag, ExamReport } from '@/types'
import ExamReportCard from '@/components/exam/ExamReportCard.vue'
import AirportTestJobResult from './AirportTestJobResult.vue'
import { scoreLevel } from '@/components/exam/stability'
import {
  kindLabel,
  statusMeta,
  isRunning,
  parseProgress,
  parseJobParams,
  scopeLabel
} from './jobmeta'

// 任务详情抽屉(「抽屉看、对话办」纪律,由 dialog 改抽屉,critique P1):
// 展示 jobs 表全字段 + 启动参数(折叠);
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
const refreshDiags = ref<RefreshFetchDiag[]>([])
const refreshLoading = ref(false)

const loadRefreshDetail = async (jobId: number) => {
  refreshLoading.value = true
  refreshRun.value = null
  refreshEvents.value = []
  refreshDiags.value = []
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
      refreshDiags.value = detail.diags || []
    }
  } catch {
    // 详情加载失败不阻塞弹框主信息
  } finally {
    refreshLoading.value = false
  }
}

// 体检结果区:exam 单份 / batch_exam 按节点聚合;无结果 kind 不展示本区(isExamKind 把关)。
const isExamKind = computed(() => props.job?.kind === 'exam' || props.job?.kind === 'batch_exam')

const examResult = ref<JobResult | null>(null)
const examLoading = ref(false)
// batch_exam 逐份展开状态(node_key 列表)
const expandedReports = ref<string[]>([])

// ExamReportRow 体检报告条目视图模型(展平 entry,模板不直接碰嵌套可空)。
interface ExamReportRow {
  nodeKey: string
  fallback: boolean
  score: number | null
  createdAt: string
  report: ExamReport | null
}

const toRow = (r: ExamJobReport): ExamReportRow => ({
  nodeKey: r.node_key,
  fallback: r.fallback,
  score: r.entry?.report.stability?.score ?? null,
  createdAt: r.entry?.created_at ?? '',
  report: r.entry?.report ?? null
})

const reportRows = computed<ExamReportRow[]>(() => (examResult.value?.reports ?? []).map(toRow))

const loadExamResult = async (jobId: number) => {
  examLoading.value = true
  examResult.value = null
  expandedReports.value = []
  try {
    examResult.value = await getJobResult(jobId)
  } catch {
    // 结果加载失败不阻塞弹框主信息(全局拦截器已提示)
  } finally {
    examLoading.value = false
  }
}

const toggleReport = (nodeKey: string) => {
  expandedReports.value = expandedReports.value.includes(nodeKey)
    ? expandedReports.value.filter((k) => k !== nodeKey)
    : [...expandedReports.value, nodeKey]
}

// 报告类任务(体检/机场测试)详情统一 640px 抽屉档(含子表宽度档,DESIGN.md)。
const scoreTagType = (score: number): 'success' | 'warning' | 'danger' => {
  const lv = scoreLevel(score)
  if (lv === 'good') return 'success'
  if (lv === 'fair') return 'warning'
  return 'danger'
}

watch(
  () => [props.job?.id, visible.value] as const,
  ([jobId, vis]) => {
    if (!vis || !jobId) return
    if (props.job?.kind === 'refresh') {
      loadRefreshDetail(jobId)
    } else if (isExamKind.value) {
      loadExamResult(jobId)
    }
    // airport_test 的结果加载由 AirportTestJobResult 组件自持(watch jobId)
  },
  { immediate: true }
)

const eventTagType = (level: string): 'primary' | 'success' | 'warning' | 'danger' | 'info' => {
  if (level === 'error') return 'danger'
  if (level === 'warn') return 'warning'
  return 'info'
}

const formatEventTime = (ts: string) => (ts.length > 19 ? ts.slice(11, 19) : ts)
// 绝对时间统一本地化呈现(与任务列表同一手法),不裸渲染 ISO 串
const formatTime = (t: string) => (t ? new Date(t).toLocaleString('zh-CN') : '-')
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
.refresh-stats,
.refresh-diags,
.fallback-alert {
  margin-bottom: var(--ph-space-3);
}
.diag-fail-tag {
  margin-left: var(--ph-space-1, 4px);
}
.refresh-events {
  max-height: 320px;
  overflow: auto;
  padding-left: 2px;
}
.batch-report-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.batch-report-item {
  border-bottom: 1px solid var(--ph-border-light);
}
.batch-report-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  width: 100%;
  padding: var(--ph-space-3) var(--ph-space-1);
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font: inherit;
  color: inherit;
}
.batch-report-row:hover {
  background: var(--ph-bg-hover);
}
.batch-report-key {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.batch-report-time {
  flex-shrink: 0;
  font-size: var(--ph-text-xs);
}
.batch-report-caret {
  flex-shrink: 0;
  transition: transform 0.15s ease;
  color: var(--ph-text-secondary);
}
.batch-report-caret.is-open {
  transform: rotate(90deg);
}
.batch-report-card {
  margin: 0 0 var(--ph-space-3);
}
</style>
