<template>
  <el-breadcrumb class="ph-breadcrumb" separator="/">
    <el-breadcrumb-item v-for="item in items" :key="item.path" :to="{ path: item.path }">
      {{ item.title }}
    </el-breadcrumb-item>
  </el-breadcrumb>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { HOME_PATH } from '../nav'

const route = useRoute()

// 从当前路由 matched 链派生面包屑,仅取带 title 的层级;非首页时前置"首页"级
const items = computed(() => {
  const crumbs = route.matched
    .filter((r) => r.meta && r.meta.title)
    .map((r) => ({ path: r.path, title: r.meta.title as string }))
  return route.path === HOME_PATH ? crumbs : [{ path: HOME_PATH, title: '首页' }, ...crumbs]
})
</script>

<style scoped>
.ph-breadcrumb {
  display: flex;
  align-items: center;
  font-size: 14px;
}
</style>
