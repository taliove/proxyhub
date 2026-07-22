<template>
  <!-- 主页装配层:只做模块排序与纵向间距,业务逻辑全部下沉到各模块组件/ composable。 -->
  <div class="dashboard">
    <PageHeader />
    <StatCards />
    <AlertPanel />
    <TopNodes />
    <PullStats />
    <QuickEndpoints />
    <ActivityFeed />
  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/PageHeader.vue'
import StatCards from './components/StatCards.vue'
import AlertPanel from './components/AlertPanel.vue'
import TopNodes from './components/TopNodes.vue'
import PullStats from './components/PullStats.vue'
import QuickEndpoints from './components/QuickEndpoints.vue'
import ActivityFeed from './components/ActivityFeed.vue'
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-4);
}
</style>
