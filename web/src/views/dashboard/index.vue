<template>
  <!-- 主页装配层:只做模块排序与纵向间距,业务逻辑全部下沉到各模块组件/ composable。
       装配约定:5 个模块按最终形态引用,后续模块 ticket(03-06)只替换各自组件内部实现。 -->
  <div class="dashboard">
    <StatCards />
    <AlertPanel />
    <TopNodes />
    <QuickEndpoints />
    <ActivityFeed />
  </div>
</template>

<script setup lang="ts">
import StatCards from './components/StatCards.vue'
import AlertPanel from './components/AlertPanel.vue'
import TopNodes from './components/TopNodes.vue'
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
