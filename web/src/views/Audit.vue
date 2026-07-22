<template>
  <div>
    <PageHeader>
      <div class="filter-bar">
        <el-select
          v-model="filterTypes"
          multiple
          collapse-tags
          placeholder="事件类型"
          class="ctl-types"
          @change="reload"
        >
          <el-option label="登录成功" value="login_success" />
          <el-option label="登录失败" value="login_failure" />
          <el-option label="蜜罐封禁" value="honeypot_ban" />
          <el-option label="阈值封禁" value="threshold_ban" />
        </el-select>
        <el-input
          v-model="filterIP"
          placeholder="按 IP 搜索"
          class="ctl-ip"
          clearable
          @change="reload"
        />
        <el-select v-model="timeRange" class="ctl-range" @change="reload">
          <el-option label="最近 24h" value="24h" />
          <el-option label="最近 7 天" value="7d" />
          <el-option label="最近 30 天" value="30d" />
          <el-option label="全部" value="all" />
        </el-select>
      </div>
    </PageHeader>

    <!-- 审计事件流水 -->
    <el-card>
      <el-table v-loading="loading" :data="events">
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="事件" width="120">
          <template #default="{ row }">
            <el-tag :type="eventTag(row.event_type)" size="small">{{
              eventLabel(row.event_type)
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP" width="150" />
        <el-table-column prop="username" label="用户名" width="150" show-overflow-tooltip />
        <el-table-column prop="detail" label="详情" show-overflow-tooltip />
      </el-table>

      <div class="pager">
        <el-pagination
          layout="total, prev, pager, next"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="onPageChange"
        />
      </div>
    </el-card>

    <!-- 当前封禁 IP -->
    <el-card class="banned-card">
      <template #header>
        <div class="card-header">
          <span>当前封禁 IP</span>
          <el-button link @click="loadBanned">刷新</el-button>
        </div>
      </template>
      <el-table v-loading="bannedLoading" :data="banned">
        <el-table-column prop="ip" label="IP" width="180" />
        <el-table-column prop="fail_count" label="失败次数" width="120" />
        <el-table-column label="封禁截止" width="200">
          <template #default="{ row }">
            <span v-if="isBanned(row)">{{ formatTime(row.banned_until) }}</span>
            <el-tag v-else type="info" size="small">未封禁</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作">
          <template #default="{ row }">
            <el-button v-if="isBanned(row)" link type="warning" @click="unban(row)">解封</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!bannedLoading && banned.length === 0"
        description="暂无封禁记录"
        :image-size="60"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'

interface AuditEvent {
  event_type: string
  ip: string
  username: string
  detail: string
  created_at: string
}
interface BannedIP {
  ip: string
  fail_count: number
  banned_until: string
}

const events = ref<AuditEvent[]>([])
const total = ref(0)
const loading = ref(false)
const page = ref(1)
const pageSize = 50

const filterTypes = ref<string[]>([])
const filterIP = ref('')
const timeRange = ref('7d')

const banned = ref<BannedIP[]>([])
const bannedLoading = ref(false)

const load = async () => {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (filterTypes.value.length) params.set('event_type', filterTypes.value.join(','))
    if (filterIP.value.trim()) params.set('ip', filterIP.value.trim())
    params.set('time_range', timeRange.value)
    params.set('limit', String(pageSize))
    params.set('offset', String((page.value - 1) * pageSize))
    const data = await client.get<unknown, { events: AuditEvent[]; total: number }>(
      `/audit/events?${params}`
    )
    events.value = data.events || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

const reload = () => {
  page.value = 1
  load()
}

const onPageChange = (p: number) => {
  page.value = p
  load()
}

const loadBanned = async () => {
  bannedLoading.value = true
  try {
    const data = await client.get<unknown, { banned: BannedIP[] }>('/audit/banned')
    banned.value = data.banned || []
  } finally {
    bannedLoading.value = false
  }
}

const unban = async (row: BannedIP) => {
  await client.post('/audit/unban', { ip: row.ip })
  ElMessage.success('已解封')
  loadBanned()
}

const isBanned = (row: BannedIP) => {
  if (!row.banned_until) return false
  return new Date(row.banned_until).getTime() > Date.now()
}

const formatTime = (t: string) => (t ? new Date(t).toLocaleString('zh-CN') : '-')

const EVENT_META: Record<string, { label: string; tag: string }> = {
  login_success: { label: '登录成功', tag: 'success' },
  login_failure: { label: '登录失败', tag: 'warning' },
  honeypot_ban: { label: '蜜罐封禁', tag: 'danger' },
  threshold_ban: { label: '阈值封禁', tag: 'danger' }
}
const eventLabel = (t: string) => EVENT_META[t]?.label || t
const eventTag = (t: string) => EVENT_META[t]?.tag || 'info'

onMounted(() => {
  load()
  loadBanned()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.filter-bar {
  display: flex;
  gap: var(--ph-space-2);
}
.ctl-types {
  width: 220px;
}
.ctl-ip {
  width: 160px;
}
.ctl-range {
  width: 120px;
}
.pager {
  margin-top: var(--ph-space-3);
  text-align: right;
}
.banned-card {
  margin-top: var(--ph-space-5);
}
</style>
