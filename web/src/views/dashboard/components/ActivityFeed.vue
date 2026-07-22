<template>
  <el-card class="activity-feed" shadow="never">
    <template #header>
      <span class="panel-title">活动流水</span>
    </template>
    <div v-if="loading" class="panel-empty">加载中...</div>
    <div v-else-if="jobs.length === 0" class="panel-empty">暂无任务</div>
    <ul v-else class="feed-list">
      <li v-for="job in jobs" :key="job.id" class="feed-item" @click="goJob(job)">
        <span class="item-kind">{{ kindLabel(job.kind) }}</span>
        <el-tag :type="statusMeta(job.status).tag" size="small" class="item-status">
          {{ statusMeta(job.status).label }}
        </el-tag>
        <span class="item-scope">{{ scopeLabel(job) }}</span>
        <span class="item-trigger">{{ jobTrigger(job) }}</span>
        <span class="item-time">{{ job.updated_at }}</span>
      </li>
    </ul>
  </el-card>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import type { Job } from '@/api/jobs'
import { kindLabel, statusMeta, scopeLabel, jobTrigger } from '@/views/jobs/jobmeta'
import { useActivityFeed } from '../composables/useActivityFeed'

const { jobs, loading } = useActivityFeed()
const router = useRouter()

// 任务条目直达任务中心详情(?id= 定位自动打开任务详情,ticket 0023)
const goJob = (job: Job) => {
  router.push({ name: 'Jobs', query: { id: String(job.id) } })
}
</script>

<style scoped>
.activity-feed {
  border-radius: var(--ph-radius-lg);
}
.panel-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.panel-empty {
  padding: var(--ph-space-6) 0;
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-placeholder);
}
.feed-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.feed-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  padding: var(--ph-space-2) 0;
  cursor: pointer;
}
.feed-item + .feed-item {
  border-top: 1px solid var(--ph-border-light);
}
.feed-item:hover .item-kind {
  color: var(--ph-color-primary);
}
.item-kind {
  flex-shrink: 0;
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.item-status {
  flex-shrink: 0;
}
.item-scope {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.item-trigger {
  flex-shrink: 0;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.item-time {
  flex-shrink: 0;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
</style>
