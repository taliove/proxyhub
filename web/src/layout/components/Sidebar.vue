<template>
  <div class="ph-sidebar" :class="{ 'is-collapsed': collapsed }">
    <div class="ph-logo">
      <img class="ph-logo__mark" src="/proxyhub-icon.png" alt="" />
      <span v-if="!collapsed" class="ph-logo__text">ProxyHub</span>
    </div>

    <el-menu
      :default-active="activePath"
      :collapse="collapsed"
      :collapse-transition="false"
      router
      class="ph-menu"
    >
      <template v-for="item in menu" :key="item.path">
        <el-divider v-if="item.divider" class="menu-divider" />
        <el-menu-item :index="item.path" @click="emit('navigate')">
          <el-icon><component :is="item.icon" /></el-icon>
          <template #title>{{ item.title }}</template>
        </el-menu-item>
      </template>
    </el-menu>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getMenuItems } from '../nav'

defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ navigate: [] }>()

const route = useRoute()
const router = useRouter()

const activePath = computed(() => route.path)

// 菜单从路由表派生(单一来源见 nav.ts)
const menu = computed(() => getMenuItems(router))
</script>

<style scoped>
.ph-sidebar {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--ph-bg-sidebar);
  border-right: 1px solid var(--ph-border-light);
}

.ph-logo {
  height: var(--ph-header-height);
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  padding: 0 15px;
  gap: 8px;
  border-bottom: 1px solid var(--ph-border-light);
  overflow: hidden;
}

.ph-logo__mark {
  width: 30px;
  height: 30px;
  flex: none;
  object-fit: contain;
}

.ph-logo__text {
  font-size: 18px;
  font-weight: 700;
  color: var(--ph-primary);
  white-space: nowrap;
  letter-spacing: 0.5px;
}

.is-collapsed .ph-logo {
  justify-content: center;
  padding: 0;
}

.ph-menu {
  flex: 1;
  border-right: none;
  overflow-x: hidden;
  overflow-y: auto;
}

/* 折叠态下 el-menu 需要固定宽度以对齐图标 */
.ph-menu:not(.el-menu--collapse) {
  width: 100%;
}

.ph-menu .el-menu-item {
  margin: 4px 8px;
  border-radius: var(--ph-radius-sm);
  height: 44px;
}

.ph-menu .el-menu-item.is-active {
  background: color-mix(in srgb, var(--ph-primary) 12%, transparent);
  color: var(--ph-primary);
  font-weight: 600;
}

.menu-divider {
  margin: 8px 12px;
  border-color: var(--ph-border-light);
}
</style>
