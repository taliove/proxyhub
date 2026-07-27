import type { Router } from 'vue-router'
import { getActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

// 导航相关的单一来源:路径归一化 + 分组菜单派生,Sidebar 与页首共用路由 meta
export const HOME_PATH = '/'

// 分组键:每个导航项归属且仅归属一个组
export type NavGroupKey = 'overview' | 'resources' | 'config'

export interface NavGroup {
  key: NavGroupKey
  label: string
}

// 组序即日常动线:总览 / 资源 / 配置
export const NAV_GROUPS: readonly NavGroup[] = [
  { key: 'overview', label: '总览' },
  { key: 'resources', label: '资源' },
  { key: 'config', label: '配置' }
]

export interface NavItem {
  path: string
  title: string
  icon: string
  group: NavGroupKey
}

export interface NavSection extends NavGroup {
  items: NavItem[]
}

// 子路由 path 归一化为绝对路径('' → '/')
export function toAbsolutePath(childPath: string): string {
  return childPath === '' ? HOME_PATH : `/${childPath}`
}

// 从根布局路由的子路由派生分组菜单(带 meta.title 的才纳入;
// meta.group 缺省归"配置"组,空组不渲染;
// meta.requiresSuperAdmin 的项仅对超管可见)
export function getMenuSections(router: Router): NavSection[] {
  // useAuthStore requires an active pinia; when called outside a component
  // (e.g. pure unit tests without pinia) admin items degrade to hidden.
  const isSuperAdmin = getActivePinia() ? useAuthStore().isSuperAdmin : false
  const root = router.options.routes.find((r) => r.path === HOME_PATH)
  const children = root?.children ?? []
  const items: NavItem[] = children
    .filter((c) => c.meta && c.meta.title)
    .filter((c) => !c.meta!.requiresSuperAdmin || isSuperAdmin)
    .map((c) => ({
      path: toAbsolutePath(c.path),
      title: c.meta!.title as string,
      icon: (c.meta!.icon as string) || 'Menu',
      group: (c.meta!.group as NavGroupKey) || 'config'
    }))
  return NAV_GROUPS.map((group) => ({
    ...group,
    items: items.filter((item) => item.group === group.key)
  })).filter((section) => section.items.length > 0)
}
