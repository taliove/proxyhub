<template>
  <el-container class="ph-layout">
    <!-- 桌面:固定侧边栏 -->
    <el-aside v-if="!isMobile" class="ph-aside" :width="asideWidth">
      <Sidebar :collapsed="layout.sidebarCollapsed" />
    </el-aside>

    <!-- 移动:抽屉侧边栏 -->
    <el-drawer
      v-else
      v-model="drawerOpen"
      direction="ltr"
      :with-header="false"
      :size="210"
      class="ph-drawer"
    >
      <Sidebar :collapsed="false" @navigate="layout.setMobileDrawer(false)" />
    </el-drawer>

    <el-container class="ph-body">
      <el-header class="ph-header" :height="'52px'">
        <Navbar :collapsed="navCollapsed" @toggle="onToggle" />
      </el-header>

      <TagsView />

      <el-main class="ph-main">
        <router-view v-slot="{ Component }">
          <transition name="ph-fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { useLayoutStore } from '@/stores/layout'
import Sidebar from './components/Sidebar.vue'
import Navbar from './components/Navbar.vue'
import TagsView from './components/TagsView.vue'

const layout = useLayoutStore()

const MOBILE_QUERY = '(max-width: 768px)'
const isMobile = ref(false)
let mql: MediaQueryList | null = null

function onMediaChange(e: MediaQueryList | MediaQueryListEvent): void {
  isMobile.value = e.matches
  // 切到桌面时收起抽屉,避免残留
  if (!e.matches) layout.setMobileDrawer(false)
}

const drawerOpen = computed({
  get: () => layout.mobileDrawerOpen,
  set: (v: boolean) => layout.setMobileDrawer(v)
})

const asideWidth = computed(() =>
  layout.sidebarCollapsed ? 'var(--ph-sidebar-width-collapsed)' : 'var(--ph-sidebar-width)'
)

// Navbar 折叠图标语义:桌面反映折叠态,移动端始终显示"展开(打开菜单)"
const navCollapsed = computed(() => (isMobile.value ? true : layout.sidebarCollapsed))

function onToggle(): void {
  if (isMobile.value) layout.setMobileDrawer(!layout.mobileDrawerOpen)
  else layout.toggleSidebar()
}

onMounted(() => {
  layout.initTheme()
  mql = window.matchMedia(MOBILE_QUERY)
  onMediaChange(mql)
  mql.addEventListener('change', onMediaChange)
})

onBeforeUnmount(() => {
  mql?.removeEventListener('change', onMediaChange)
})
</script>

<style scoped>
.ph-layout {
  height: 100vh;
}

.ph-aside {
  transition: width var(--ph-transition);
  overflow: hidden;
}

.ph-body {
  overflow: hidden;
}

.ph-header {
  background: var(--ph-bg-surface);
  border-bottom: 1px solid var(--ph-border-light);
  box-shadow: var(--ph-shadow-sm);
  padding: 0;
  z-index: 10;
}

.ph-main {
  background: var(--ph-bg-page);
  padding: 16px;
  overflow-y: auto;
}

/* 页面切换淡入淡出 */
.ph-fade-enter-active,
.ph-fade-leave-active {
  transition: opacity 0.15s ease;
}
.ph-fade-enter-from,
.ph-fade-leave-to {
  opacity: 0;
}
</style>

<style>
/* 抽屉内容去除内边距,让 Sidebar 铺满 */
.ph-drawer .el-drawer__body {
  padding: 0;
}
</style>
