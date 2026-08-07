import { ref } from 'vue'
import type { Node } from '@/types'
import { setNodeFavorite } from '@/api/nodes'
import type { NodeFilterCriteria } from '../predicates'
import { SELF_HOSTED } from '../utils'

// 节点收藏(issue #83):行内 star 的状态管理 + 乐观更新。
// 服务端持久(node_overrides.favorite),列表行的 favorite 由 /api/nodes 透出;
// 本模块只在 toggle 瞬间持"本地乐观覆盖",池重载后以服务端值为准(resetOverrides)。

// applyFavoriteOverrides 把乐观覆盖叠到行集上(纯函数,不改入参,返回新数组)。
export const applyFavoriteOverrides = <T extends Node>(
  rows: T[],
  overrides: Record<string, boolean>
): T[] => {
  if (Object.keys(overrides).length === 0) return rows
  return rows.map((n) => (n.node_key in overrides ? { ...n, favorite: overrides[n.node_key] } : n))
}

// 快捷 Tab(自建专区 + 已收藏,issue #83):全部 / 自建节点 / 已收藏 三态,
// 直达既有 source=自建 深链语义(SELF_HOSTED 常量)与收藏筛选。
export type NodeQuickTab = 'all' | 'self' | 'favorite'

// quickTabOf 从筛选条件反推当前 Tab(来源下拉已选自建 / 收藏筛选已开时高亮对应 Tab)。
export const quickTabOf = (c: Pick<NodeFilterCriteria, 'source' | 'favorite'>): NodeQuickTab => {
  if (c.source === SELF_HOSTED) return 'self'
  if (c.favorite === true) return 'favorite'
  return 'all'
}

// applyQuickTab 产出 Tab 切换的筛选补丁(纯函数;互斥语义:进自建/收藏 Tab 清另一维)。
export const applyQuickTab = (
  tab: NodeQuickTab
): Pick<NodeFilterCriteria, 'source' | 'favorite'> => {
  switch (tab) {
    case 'self':
      return { source: SELF_HOSTED, favorite: null }
    case 'favorite':
      return { source: '', favorite: true }
    default:
      return { source: '', favorite: null }
  }
}

export function useNodeFavorites() {
  // node_key -> 乐观覆盖值;仅在 toggle 后到下次池重载之间生效
  const favoriteOverrides = ref<Record<string, boolean>>({})

  // toggleFavorite 行内 star:先乐观落覆盖,再调 API 持久化;失败回滚
  // (错误 toast 由 client 拦截器统一负责,这里只管状态一致性)。
  const toggleFavorite = async (row: Node) => {
    const key = row.node_key
    const next = !(favoriteOverrides.value[key] ?? row.favorite ?? false)
    favoriteOverrides.value = { ...favoriteOverrides.value, [key]: next }
    try {
      await setNodeFavorite(key, next)
    } catch {
      const { [key]: _dropped, ...rest } = favoriteOverrides.value
      favoriteOverrides.value = rest
    }
  }

  // resetOverrides 池重载后调用:服务端值已是最新,丢弃本地覆盖避免陈旧分叉。
  const resetOverrides = () => {
    favoriteOverrides.value = {}
  }

  return { favoriteOverrides, toggleFavorite, resetOverrides }
}
