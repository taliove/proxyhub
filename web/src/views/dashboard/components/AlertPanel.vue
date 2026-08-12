<template>
  <el-card class="alert-panel" shadow="never">
    <template #header>
      <span class="panel-title">异常告警</span>
    </template>
    <div v-if="loading" class="panel-empty">加载中……</div>
    <template v-else>
      <div v-if="alerts.length === 0" class="panel-empty">一切正常</div>
      <ul v-else class="alert-list">
        <li v-for="a in alerts" :key="a.key" class="alert-item" @click="go(a)">
          <el-tag :type="a.severity" size="small" class="item-tag">
            {{ categoryLabel(a.category) }}
          </el-tag>
          <span class="item-text">{{ a.text }}</span>
        </li>
      </ul>
      <!-- 未测试机场不算异常,弱提示独立成行(有无异常都展示) -->
      <div v-if="untestedCount > 0" class="panel-hint" @click="goRoute('Airports')">
        {{ untestedCount }} 个机场尚未测试
      </div>
    </template>
  </el-card>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAlertPanel, type AlertCategory, type AlertItem } from '../composables/useAlertPanel'

const { alerts, untestedCount, loading } = useAlertPanel()
const router = useRouter()

const CATEGORY_LABELS: Record<AlertCategory, string> = {
  airport: '机场',
  job: '任务',
  banned: '封禁',
  audit: '审计'
}

const categoryLabel = (c: AlertCategory) => CATEGORY_LABELS[c]

// 任务类异常带 ?id= 直达任务详情(ticket 0023);其余类别跳对应列表页
const go = (a: AlertItem) => {
  if (a.category === 'job' && a.jobId !== undefined) {
    router.push({ name: a.route, query: { id: String(a.jobId) } })
  } else {
    goRoute(a.route)
  }
}

const goRoute = (route: string) => {
  router.push({ name: route })
}
</script>

<style scoped>
.alert-panel {
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
  color: var(--ph-text-secondary);
}
.alert-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.alert-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  padding: var(--ph-space-2) 0;
  cursor: pointer;
}
.alert-item + .alert-item {
  border-top: 1px solid var(--ph-border-light);
}
.alert-item:hover .item-text {
  color: var(--ph-color-primary);
}
.item-tag {
  flex-shrink: 0;
}
.item-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-regular);
}
.panel-hint {
  margin-top: var(--ph-space-2);
  padding-top: var(--ph-space-2);
  border-top: 1px solid var(--ph-border-light);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  cursor: pointer;
}
.panel-hint:hover {
  color: var(--ph-color-primary);
}
</style>
