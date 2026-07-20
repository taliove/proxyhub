<template>
  <el-card>
    <template #header>
      <span>节点管理</span>
    </template>

    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <el-tab-pane label="机场节点" name="airport">
        <AirportNodesTab />
      </el-tab-pane>
      <el-tab-pane label="自建节点" name="self">
        <SelfNodesTab />
      </el-tab-pane>
      <el-tab-pane label="分发节点" name="distribution">
        <DistributionNodesTab />
      </el-tab-pane>
    </el-tabs>
  </el-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SelfNodesTab from './components/SelfNodesTab.vue'
import DistributionNodesTab from './components/DistributionNodesTab.vue'
import AirportNodesTab from './components/AirportNodesTab.vue'

const router = useRouter()
const route = useRoute()

// Tab 管理(URL query 同步)
const activeTab = ref<'airport' | 'self' | 'distribution'>('airport')
const handleTabChange = (tab: string) => {
  const validTab = tab === 'self' ? 'self' : tab === 'distribution' ? 'distribution' : 'airport'
  activeTab.value = validTab
  router.push({ query: { tab: validTab === 'airport' ? undefined : validTab } })
}

onMounted(() => {
  const tabParam = route.query.tab
  if (tabParam === 'self') activeTab.value = 'self'
  else if (tabParam === 'distribution') activeTab.value = 'distribution'
  else if (tabParam && tabParam !== 'airport') router.replace({ query: { tab: undefined } })
})
</script>
