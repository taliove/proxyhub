<template>
  <div class="ph-tags" @contextmenu.prevent>
    <div class="ph-tags__scroll">
      <router-link
        v-for="tag in layout.visitedTags"
        :key="tag.path"
        :to="tag.path"
        class="ph-tag"
        :class="{ 'is-active': tag.path === activePath }"
        @contextmenu.prevent="openMenu(tag, $event)"
      >
        <span class="ph-tag__dot" />
        {{ tag.title }}
        <el-icon
          v-if="!isAffix(tag.path)"
          class="ph-tag__close"
          @click.prevent.stop="closeTag(tag)"
        >
          <Close />
        </el-icon>
      </router-link>
    </div>

    <ul
      v-show="menu.visible"
      class="ph-tags__menu"
      :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    >
      <li @click="closeOthers">关闭其他</li>
      <li @click="closeAll">关闭全部</li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Close } from '@element-plus/icons-vue'
import { useLayoutStore, type RouteTag } from '@/stores/layout'
import { HOME_PATH } from '../nav'

const route = useRoute()
const router = useRouter()
const layout = useLayoutStore()

const activePath = computed(() => route.path)

const menu = reactive({ visible: false, x: 0, y: 0, target: null as RouteTag | null })

function isAffix(path: string): boolean {
  return path === HOME_PATH
}

// 记录当前路由为标签
function record(): void {
  const title = (route.meta.title as string) || (route.name as string) || route.path
  if (!title) return
  layout.addTag({ path: route.path, title, name: (route.name as string) || route.path })
}

watch(() => route.path, record, { immediate: true })

function closeTag(tag: RouteTag): void {
  const wasActive = tag.path === activePath.value
  const idx = layout.visitedTags.findIndex((t) => t.path === tag.path)
  layout.removeTag(tag.path)
  if (wasActive) {
    // 跳到相邻标签:优先右侧(移除后右邻居落到 idx),否则左侧,都无则回首页
    const tags = layout.visitedTags
    const neighbor = tags[idx] || tags[idx - 1]
    router.push(neighbor ? neighbor.path : HOME_PATH)
  }
}

function openMenu(tag: RouteTag, e: MouseEvent): void {
  menu.target = tag
  menu.x = e.clientX
  menu.y = e.clientY
  menu.visible = true
}

function closeMenu(): void {
  menu.visible = false
}

function closeOthers(): void {
  if (!menu.target) return
  layout.removeOtherTags(menu.target.path)
  if (menu.target.path !== activePath.value) router.push(menu.target.path)
  closeMenu()
}

function closeAll(): void {
  layout.clearTags()
  if (activePath.value !== HOME_PATH) {
    router.push(HOME_PATH) // 导航后 watch(route) 会自动记录首页标签
  } else {
    record() // 已在首页,直接重建首页标签,最终只保留首页一个
  }
  closeMenu()
}

onMounted(() => document.addEventListener('click', closeMenu))
onBeforeUnmount(() => document.removeEventListener('click', closeMenu))
</script>

<style scoped>
.ph-tags {
  height: var(--ph-tags-height);
  background: var(--ph-bg-surface);
  border-bottom: 1px solid var(--ph-border-light);
  display: flex;
  align-items: center;
  padding: 0 12px;
}

.ph-tags__scroll {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: none;
}
.ph-tags__scroll::-webkit-scrollbar {
  display: none;
}

.ph-tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  font-size: 12px;
  color: var(--ph-text-regular);
  background: var(--ph-bg-surface);
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
  text-decoration: none;
  cursor: pointer;
  transition: all var(--ph-transition);
}

.ph-tag:hover {
  color: var(--ph-primary);
}

.ph-tag.is-active {
  color: #fff;
  background: var(--ph-primary);
  border-color: var(--ph-primary);
}

.ph-tag__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.7;
}

.ph-tag__close {
  font-size: 12px;
  border-radius: 50%;
}
.ph-tag__close:hover {
  background: rgba(0, 0, 0, 0.15);
}

.ph-tags__menu {
  position: fixed;
  z-index: 3000;
  background: var(--ph-bg-surface);
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius-sm);
  box-shadow: var(--ph-shadow);
  padding: 4px 0;
  list-style: none;
  min-width: 120px;
}

.ph-tags__menu li {
  padding: 8px 16px;
  font-size: 13px;
  color: var(--ph-text-regular);
  cursor: pointer;
}

.ph-tags__menu li:hover {
  background: var(--ph-bg-hover);
  color: var(--ph-primary);
}
</style>
