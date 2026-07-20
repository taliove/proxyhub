import { defineStore } from 'pinia'
import { ref } from 'vue'

const LS_COLLAPSE = 'ph:sidebar-collapsed'
// 注意:'ph:dark' 同时被 index.html 的首屏防闪内联脚本读取,两处需保持一致
const LS_DARK = 'ph:dark'

// localStorage 读取带兜底,兼容隐私模式/禁用存储
function readBool(key: string, def: boolean): boolean {
  try {
    const v = localStorage.getItem(key)
    return v === null ? def : v === '1'
  } catch {
    return def
  }
}

function writeBool(key: string, val: boolean): void {
  try {
    localStorage.setItem(key, val ? '1' : '0')
  } catch {
    // 存储不可用时静默降级,不影响功能
  }
}

export interface RouteTag {
  path: string
  title: string
  name: string
}

export const useLayoutStore = defineStore('layout', () => {
  const sidebarCollapsed = ref(readBool(LS_COLLAPSE, false))
  const isDark = ref(readBool(LS_DARK, false))
  const mobileDrawerOpen = ref(false)
  const visitedTags = ref<RouteTag[]>([])

  function toggleSidebar(): void {
    sidebarCollapsed.value = !sidebarCollapsed.value
    writeBool(LS_COLLAPSE, sidebarCollapsed.value)
  }

  function setMobileDrawer(open: boolean): void {
    mobileDrawerOpen.value = open
  }

  function applyDark(dark: boolean): void {
    const el = document.documentElement
    if (dark) el.classList.add('dark')
    else el.classList.remove('dark')
  }

  function toggleDark(): void {
    isDark.value = !isDark.value
    writeBool(LS_DARK, isDark.value)
    applyDark(isDark.value)
  }

  // 与 index.html 的防闪脚本对齐:挂载后再确保 DOM class 与状态一致
  function initTheme(): void {
    applyDark(isDark.value)
  }

  // 以下 tags 操作均返回新数组,遵循不可变更新
  function addTag(tag: RouteTag): void {
    if (visitedTags.value.some((t) => t.path === tag.path)) return
    visitedTags.value = [...visitedTags.value, tag]
  }

  function removeTag(path: string): void {
    visitedTags.value = visitedTags.value.filter((t) => t.path !== path)
  }

  function removeOtherTags(path: string): void {
    visitedTags.value = visitedTags.value.filter((t) => t.path === path)
  }

  function clearTags(): void {
    visitedTags.value = []
  }

  return {
    sidebarCollapsed,
    isDark,
    mobileDrawerOpen,
    visitedTags,
    toggleSidebar,
    setMobileDrawer,
    toggleDark,
    initTheme,
    addTag,
    removeTag,
    removeOtherTags,
    clearTags
  }
})
