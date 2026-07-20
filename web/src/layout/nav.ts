import type { Router } from 'vue-router'

// 导航相关的单一来源:路径归一化 + 菜单派生,避免 Sidebar/Breadcrumb/TagsView 各写一份
export const HOME_PATH = '/'

export interface NavItem {
  path: string
  title: string
  icon: string
  divider?: boolean  // 在此项前显示分隔线
}

// 子路由 path 归一化为绝对路径('' → '/')
export function toAbsolutePath(childPath: string): string {
  return childPath === '' ? HOME_PATH : `/${childPath}`
}

// 从根布局路由的子路由派生菜单项(带 meta.title 的才纳入)
export function getMenuItems(router: Router): NavItem[] {
  const root = router.options.routes.find((r) => r.path === HOME_PATH)
  const children = root?.children ?? []
  return children
    .filter((c) => c.meta && c.meta.title)
    .map((c) => ({
      path: toAbsolutePath(c.path),
      title: c.meta!.title as string,
      icon: (c.meta!.icon as string) || 'Menu',
      divider: Boolean(c.meta!.divider)  // 从路由 meta 读取分隔线标记
    }))
}
