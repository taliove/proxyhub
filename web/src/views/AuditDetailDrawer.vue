<template>
  <el-drawer v-model="visible" title="事件详情" size="500px">
    <div v-if="event" class="detail-content">
      <div class="detail-row">
        <div class="detail-label">事件类型</div>
        <div class="detail-value">
          <el-tag :type="eventTag(event.event_type)" size="small">{{
            eventLabel(event.event_type)
          }}</el-tag>
        </div>
      </div>
      <div class="detail-row">
        <div class="detail-label">时间</div>
        <div class="detail-value">{{ formatTime(event.created_at) }}</div>
      </div>
      <div class="detail-row">
        <div class="detail-label">用户名</div>
        <div class="detail-value">{{ event.username || '-' }}</div>
      </div>
      <div class="detail-row">
        <div class="detail-label">IP 地址</div>
        <div class="detail-value">{{ event.ip }}</div>
      </div>
      <div class="detail-row">
        <div class="detail-label">地理位置</div>
        <div class="detail-value">{{ formatGeo(event.geo) }}</div>
      </div>
      <div class="detail-row">
        <div class="detail-label">客户端</div>
        <div class="detail-value">{{ parseUA(event.user_agent) }}</div>
      </div>
      <div v-if="event.user_agent" class="detail-row">
        <div class="detail-label">完整 UA</div>
        <div class="detail-value detail-value-wrap">{{ event.user_agent }}</div>
      </div>
      <div v-if="event.detail" class="detail-row">
        <div class="detail-label">事件详情</div>
        <div class="detail-value detail-value-wrap">{{ event.detail }}</div>
      </div>

      <el-divider />

      <div class="detail-section-title">IP 操作</div>
      <div v-if="banStatus === 'banned'" class="detail-row">
        <el-alert type="warning" :closable="false" show-icon> 该 IP 当前已被封禁 </el-alert>
        <el-button type="warning" :loading="loading" class="unban-button" @click="handleUnban">
          解封此 IP
        </el-button>
      </div>
      <div v-else class="detail-row">
        <div class="ban-controls">
          <el-select v-model="duration" placeholder="选择封禁时长" class="ban-duration-select">
            <el-option label="1 小时" value="1h" />
            <el-option label="24 小时" value="24h" />
            <el-option label="永久" value="permanent" />
          </el-select>
          <el-button type="danger" :loading="loading" @click="handleBan"> 封禁此 IP </el-button>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { eventLabel, eventTag, parseUserAgent, formatGeoLocation } from './audit-utils'

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

interface Props {
  modelValue: boolean
  event: AuditEvent | null
  bannedIPs: BannedIP[]
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  ban: [ip: string, duration: string]
  unban: [ip: string]
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const duration = ref('24h')
const loading = ref(false)

watch(
  () => props.event,
  () => {
    duration.value = '24h'
  }
)

const banStatus = computed(() => {
  if (!props.event) return 'unknown'
  const ip = props.event.ip
  const bannedRecord = props.bannedIPs.find((b) => b.ip === ip)
  if (!bannedRecord) return 'not-banned'
  const isBanned =
    bannedRecord.banned_until && new Date(bannedRecord.banned_until).getTime() > Date.now()
  return isBanned ? 'banned' : 'not-banned'
})

const handleBan = async () => {
  if (!props.event) return
  loading.value = true
  try {
    emit('ban', props.event.ip, duration.value)
  } finally {
    loading.value = false
  }
}

const handleUnban = async () => {
  if (!props.event) return
  loading.value = true
  try {
    emit('unban', props.event.ip)
  } finally {
    loading.value = false
  }
}

const formatTime = (t: string) => (t ? new Date(t).toLocaleString('zh-CN') : '-')
const parseUA = (ua: string | undefined) => parseUserAgent(ua)
const formatGeo = (geo: GeoInfo | undefined) => formatGeoLocation(geo || {})
</script>

<style scoped>
.detail-content {
  padding: var(--ph-space-2);
}
.detail-row {
  margin-bottom: var(--ph-space-4);
}
.detail-label {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  margin-bottom: var(--ph-space-1);
}
.detail-value {
  font-size: var(--ph-text-base);
  color: var(--ph-text-primary);
}
.detail-value-wrap {
  word-break: break-all;
  white-space: pre-wrap;
}
.detail-section-title {
  font-size: var(--ph-text-base);
  font-weight: 600;
  margin-bottom: var(--ph-space-3);
  color: var(--ph-text-primary);
}
.ban-controls {
  display: flex;
  gap: var(--ph-space-2);
  align-items: center;
}
.unban-button {
  margin-top: var(--ph-space-3);
}
.ban-duration-select {
  width: 200px;
}
</style>
