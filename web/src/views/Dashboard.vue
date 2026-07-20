<template>
  <div>
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card>
          <el-statistic title="总节点数" :value="stats.totalNodes" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="可用节点" :value="stats.availableNodes" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="订阅地址" :value="stats.endpoints" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="机场数量" :value="stats.airports" />
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px">
      <template #header>系统状态</template>
      <el-descriptions :column="2">
        <el-descriptions-item label="最近更新">{{ stats.lastUpdate }}</el-descriptions-item>
        <el-descriptions-item label="平均延迟">{{ stats.avgLatency }}ms</el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'

const stats = ref({
  totalNodes: 0,
  availableNodes: 0,
  endpoints: 0,
  airports: 0,
  lastUpdate: '-',
  avgLatency: 0
})

onMounted(async () => {
  const data = await client.get<any, typeof stats.value>('/dashboard/stats')
  stats.value = data
})
</script>
