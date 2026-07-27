<template>
  <div class="ph-sidebar" :class="{ 'is-collapsed': collapsed }">
    <div class="ph-logo">
      <BrandMark class="ph-logo__mark" />
      <!-- 折叠态优雅降级:隐藏字标,仅留图标 -->
      <Wordmark v-if="!collapsed" class="ph-logo__wordmark" />
    </div>

    <el-menu
      :default-active="activePath"
      :collapse="collapsed"
      :collapse-transition="false"
      router
      class="ph-menu"
    >
      <template v-for="(section, sectionIndex) in sections" :key="section.key">
        <!-- 展开态:组标签;折叠态:组间分隔线(首组前不渲染) -->
        <div v-if="!collapsed" class="nav-group-label" :class="{ 'is-first': sectionIndex === 0 }">
          {{ section.label }}
        </div>
        <el-divider v-else-if="sectionIndex > 0" class="nav-group-divider" />
        <el-menu-item
          v-for="item in section.items"
          :key="item.path"
          :index="item.path"
          @click="emit('navigate')"
        >
          <el-icon><component :is="navIcon(item.icon)" :size="20" /></el-icon>
          <template #title>{{ item.title }}</template>
        </el-menu-item>
      </template>
    </el-menu>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  IconDashboard,
  IconCloud,
  IconCpu,
  IconLink,
  IconTemplate,
  IconShieldCheck,
  IconListCheck,
  IconSettings,
  IconGauge,
  IconUsers,
  IconMenu2
} from '@tabler/icons-vue'
import BrandMark from '@/components/BrandMark.vue'
import Wordmark from '@/components/Wordmark.vue'
import { getMenuSections } from '../nav'

// 导航图标:route meta.icon 名(EP 旧名)→ Tabler 组件(2px 描边,存在感强一档)
const NAV_ICONS: Record<string, unknown> = {
  Monitor: IconDashboard,
  Connection: IconCloud,
  Cpu: IconCpu,
  Link: IconLink,
  Document: IconTemplate,
  Warning: IconShieldCheck,
  List: IconListCheck,
  Setting: IconSettings,
  Odometer: IconGauge,
  User: IconUsers
}
const navIcon = (name: string): unknown => NAV_ICONS[name] ?? IconMenu2

defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ navigate: [] }>()

const route = useRoute()
const router = useRouter()

const activePath = computed(() => route.path)

// 分组菜单从路由表派生(单一来源见 nav.ts)
const sections = computed(() => getMenuSections(router))
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
  font-size: 30px;
  flex: none;
}

.is-collapsed .ph-logo {
  justify-content: center;
  padding: 0;
}

/* 折叠态图形标收小一档,不顶满 rail */
.is-collapsed .ph-logo__mark {
  font-size: 26px;
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

/* 组标签:小号弱色,与菜单项左边距对齐 */
.nav-group-label {
  padding: var(--ph-space-4) var(--ph-space-4) var(--ph-space-1);
  font-size: var(--ph-text-xs);
  font-weight: 500;
  letter-spacing: 0.1em;
  color: var(--ph-text-placeholder);
  white-space: nowrap;
}

.nav-group-label.is-first {
  padding-top: var(--ph-space-2);
}

.nav-group-divider {
  margin: 8px 12px;
  border-color: var(--ph-border-light);
}

.ph-menu .el-menu-item {
  margin: 4px 8px;
  border-radius: var(--ph-radius-sm);
  height: 44px;
}

/* 菜单图标:EP 各字形视觉大小不一,统一放大到 20px 让细线图标更有存在感 */
.ph-menu .el-menu-item .el-icon {
  font-size: 20px;
}

/* 折叠态:EP 把菜单项内容包进 tooltip trigger(ElMenuItem 内部元素,scoped 够不到,必须 :deep),居中作用在这一层 */
.ph-menu.el-menu--collapse .el-menu-item {
  padding: 0;
}

.ph-menu.el-menu--collapse :deep(.el-menu-tooltip__trigger) {
  padding: 0;
  justify-content: center;
}

.ph-menu.el-menu--collapse .el-menu-item .el-icon {
  margin-right: 0;
}

.ph-menu .el-menu-item.is-active {
  background: color-mix(in srgb, var(--ph-primary) 12%, transparent);
  color: var(--ph-primary);
  font-weight: 600;
}
</style>
