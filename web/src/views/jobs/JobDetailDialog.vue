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
import { computed } from 'vue'
import type { Job } from '@/api/jobs'
import {
  kindLabel,
  statusMeta,
  isRunning,
  parseProgress,
  parseJobParams,
  scopeLabel
} from './jobmeta'

// 任务详情弹框:展示 jobs 表全字段 + 启动参数(折叠)。
// 数据直接取列表行(已含 params),不重复请求。
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
</style>
