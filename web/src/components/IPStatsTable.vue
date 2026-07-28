<template>
  <el-table v-loading="loading" :data="stats" size="small">
    <el-table-column prop="ip" label="IP" width="150" />
    <el-table-column label="状态" width="110">
      <template #default="{ row }">
        <el-tag :type="pullStatusTag(row.status)" size="small">{{
          pullStatusLabel(row.status)
        }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="count" label="拉取次数" width="100" />
    <el-table-column label="最后拉取" width="180">
      <template #default="{ row }">{{ formatTime(row.last_pull) }}</template>
    </el-table-column>
    <el-table-column label="地理位置">
      <template #default="{ row }">{{ formatGeo(row) }}</template>
    </el-table-column>
    <el-table-column v-if="canBan" label="操作" width="170">
      <template #default="{ row }">
        <div class="ban-cell">
          <el-select v-model="banDurations[row.ip]" size="small" class="ban-duration">
            <el-option
              v-for="opt in RULE_DURATION_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
          <el-button
            link
            type="danger"
            size="small"
            :loading="banningIP === row.ip"
            @click="onBan(row)"
            >封禁</el-button
          >
        </div>
      </template>
    </el-table-column>
  </el-table>
  <el-empty v-if="!loading && stats.length === 0" description="暂无拉取记录" :image-size="60" />
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import { createIPRule } from '@/api/ip-rules'
import { useAuthStore } from '@/stores/auth'
import { RULE_DURATION_OPTIONS, pullStatusLabel, pullStatusTag } from '@/utils/pullguard'

// 订阅 IP 明细(pull-guard ticket 06):后端按 IP + 拉取状态聚合,所以同一 IP 的
// 成功与被拦尝试是不同的行,status 列让"为什么没拿到订阅"直接可见。
// 行内封禁写 scope=sub 规则(只掐拉取,不动管理面);写完重新拉取统计,
// 让操作者看到该 IP 之后的拉取以 blacklisted 落账。

interface IPStat {
  ip: string
  status: string
  count: number
  last_pull: string
  country: string
  region: string
  city: string
  isp: string
}

const props = defineProps<{ endpointId: number }>()

// 封禁写的是 /api/admin/ip-rules(adminGuard)。这张表也出现在普通用户的订阅抽屉里,
// 所以非超管不渲染操作列 —— 否则按钮必然 403,是个假动作。
const auth = useAuthStore()
const canBan = computed(() => auth.isSuperAdmin)

const stats = ref<IPStat[]>([])
const loading = ref(false)
// 每行独立的时长选择:同一张表里可能既要临时限一小时,也要长期拉黑。
const banDurations = reactive<Record<string, string>>({})
const banningIP = ref('')

const load = async () => {
  if (!props.endpointId) return
  loading.value = true
  try {
    const data = await client.get<unknown, IPStat[]>(`/endpoints/${props.endpointId}/stats`)
    stats.value = data || []
    for (const row of stats.value) {
      if (!banDurations[row.ip]) banDurations[row.ip] = '24h'
    }
  } finally {
    loading.value = false
  }
}

const onBan = async (row: IPStat) => {
  banningIP.value = row.ip
  try {
    await createIPRule({
      ip_or_cidr: row.ip,
      scope: 'sub',
      duration: banDurations[row.ip] || '24h',
      comment: '订阅 IP 明细行内封禁'
    })
    ElMessage.success('已加入拉取黑名单')
    await load()
  } finally {
    banningIP.value = ''
  }
}

const formatTime = (t: string) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const formatGeo = (row: IPStat) => {
  const parts = [row.country, row.region, row.city].filter(Boolean)
  const loc = parts.join(' ') || '未知'
  return row.isp ? `${loc} / ${row.isp}` : loc
}

watch(() => props.endpointId, load)
onMounted(load)
</script>

<style scoped>
.ban-cell {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.ban-duration {
  width: 92px;
}
</style>
