<template>
  <el-table :data="stats" v-loading="loading" size="small">
    <el-table-column prop="ip" label="IP" width="150" />
    <el-table-column prop="count" label="拉取次数" width="100" />
    <el-table-column label="最后拉取" width="180">
      <template #default="{ row }">{{ formatTime(row.last_pull) }}</template>
    </el-table-column>
    <el-table-column label="地理位置">
      <template #default="{ row }">{{ formatGeo(row) }}</template>
    </el-table-column>
  </el-table>
  <el-empty v-if="!loading && stats.length === 0" description="暂无拉取记录" :image-size="60" />
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import client from '@/api/client'

interface IPStat {
  ip: string
  count: number
  last_pull: string
  country: string
  region: string
  city: string
  isp: string
}

const props = defineProps<{ endpointId: number }>()

const stats = ref<IPStat[]>([])
const loading = ref(false)

const load = async () => {
  if (!props.endpointId) return
  loading.value = true
  try {
    const data = await client.get<any, IPStat[]>(`/endpoints/${props.endpointId}/stats`)
    stats.value = data || []
  } finally {
    loading.value = false
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
