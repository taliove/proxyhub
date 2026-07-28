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
          <el-option
            v-for="opt in EVENT_FILTER_OPTIONS"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
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
      <el-table v-loading="loading" :data="events" @row-click="showDetail">
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
        <el-table-column label="客户端" width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ parseUA(row.user_agent) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" width="150" show-overflow-tooltip />
        <el-table-column label="详情" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="detail-cell">
              <!-- login_success 的二段标记提到徽标里:区分真实过了 MFA 与受信 IP 免验 -->
              <el-tag
                v-for="badge in mfaBadges(row)"
                :key="badge.marker"
                :type="badge.tag"
                size="small"
                effect="plain"
                class="mfa-badge"
                >{{ badge.label }}</el-tag
              >
              <span>{{ detailOf(row) }}</span>
            </span>
          </template>
        </el-table-column>
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

    <!-- 事件详情抽屉 -->
    <AuditDetailDrawer
      v-model="detailVisible"
      :event="selectedEvent"
      :banned-i-ps="banned"
      @ban="onBan"
      @unban="onUnban"
    />

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

    <!-- IP 规则(整站拒止 / 拉取黑名单) -->
    <IPRuleList />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import AuditDetailDrawer from './AuditDetailDrawer.vue'
import IPRuleList from './IPRuleList.vue'
import {
  EVENT_FILTER_OPTIONS,
  detailText,
  eventLabel,
  eventTag,
  loginMFABadge,
  parseUserAgent
} from './audit-utils'

interface GeoInfo {
  country?: string
  region?: string
  city?: string
  isp?: string
}

interface AuditEvent {
  event_type: string
  ip: string
  username: string
  detail: string
  created_at: string
  user_agent?: string
  geo?: GeoInfo
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

// 详情抽屉状态
const detailVisible = ref(false)
const selectedEvent = ref<AuditEvent | null>(null)

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

// 返回数组(0 或 1 个徽标)而非可空值:模板里用 v-for 渲染,省掉非空断言。
const mfaBadges = (row: AuditEvent) => {
  const badge = loginMFABadge(row.event_type, row.detail)
  return badge ? [badge] : []
}
const detailOf = (row: AuditEvent) => detailText(row.event_type, row.detail)
const parseUA = (ua: string | undefined) => parseUserAgent(ua)

// 显示事件详情抽屉
const showDetail = (row: AuditEvent) => {
  selectedEvent.value = row
  detailVisible.value = true
}

// 封禁 IP (从抽屉触发)
const onBan = async (ip: string, duration: string) => {
  await client.post('/audit/ban', { ip, duration })
  ElMessage.success('封禁成功')
  await loadBanned()
  await load()
}

// 解封 IP (从抽屉触发)
const onUnban = async (ip: string) => {
  await client.post('/audit/unban', { ip })
  ElMessage.success('已解封')
  await loadBanned()
  await load()
}

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
  width: 240px;
}
.detail-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
/* 徽标不参与 flex 收缩,详情文字过长时优先截断文字 */
.mfa-badge {
  flex: none;
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
