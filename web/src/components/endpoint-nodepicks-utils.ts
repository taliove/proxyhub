// 订阅地址精选节点(issue #80/#85/#86)的纯函数,供选择器对话框与列表标签复用。
// 全部无副作用、不改入参,语义与后端 internal/store/node_picks.go 对齐。

import type { Node } from '@/types'

// NodePick 精选项(issue #85 对象形态):key = NodeKey;alias 可选,
// 非空时是该订阅下发命名链的最终层(仅本订阅生效);留空 = 跟随命名链。
export interface NodePick {
  key: string
  alias?: string
}

// parseNodePicks 把 endpoint.node_picks 原始 JSON 串解析为精选项列表。
// 双格式兼容(issue #85):旧格式字符串元素视为 {key, 无别名};
// 非字符串/非合法对象元素被过滤。空串/缺省/非法 JSON 均按空集
// (与后端 endpointNodePicks 降级语义一致:宁可全量,不把订阅打挂)。
export function parseNodePicks(raw?: string): NodePick[] {
  if (!raw) return []
  try {
    const arr: unknown = JSON.parse(raw)
    if (!Array.isArray(arr)) return []
    const picks: NodePick[] = []
    for (const x of arr) {
      if (typeof x === 'string') {
        picks.push({ key: x })
      } else if (x && typeof x === 'object' && typeof (x as NodePick).key === 'string') {
        const obj = x as NodePick
        picks.push({ key: obj.key, ...(obj.alias ? { alias: obj.alias } : {}) })
      }
    }
    return picks
  } catch {
    return []
  }
}

// 精选数量(0 = 未精选 = 全量)
export function nodePicksCount(ep: { node_picks?: string }): number {
  return parseNodePicks(ep.node_picks).length
}

// 列表展示文案:「精选 N 个节点」/「全量」
export function nodePicksLabel(ep: { node_picks?: string }): string {
  const n = nodePicksCount(ep)
  return n > 0 ? `精选 ${n} 个节点` : '全量'
}

// ===== 订阅列表精选状态筛选(issue #87)=====

// PicksStatusFilter 精选状态:all 全部 | full 仅全量(未配精选) | picked 仅已精选。
export type PicksStatusFilter = 'all' | 'full' | 'picked'

// filterEndpointsByPicks 按精选状态过滤订阅地址列表(前端过滤,不改接口)。
// 非法/损坏的 node_picks 按空集(=全量)归类,与解析降级语义一致。
export function filterEndpointsByPicks<T extends { node_picks?: string }>(
  endpoints: T[],
  filter: PicksStatusFilter
): T[] {
  if (filter === 'all') return endpoints
  return endpoints.filter((ep) => nodePicksCount(ep) > 0 === (filter === 'picked'))
}

// ===== 选择器池过滤与分页(issue #86)=====

// PicksPoolTab 池快捷页签:all 全部 | self 仅自建节点 | fav 仅已收藏。
// 语义与节点管理页筛选对齐(自建 = source 精确 'self-hosted',
// 收藏 = favorite 为 true,缺省按未收藏)。
export type PicksPoolTab = 'all' | 'self' | 'fav'

// filterPicksPool 页签 + 关键字(名称/显示名/来源/地区子串,大小写不敏感)过滤池节点。
// 关键字跨全量池生效(不只当前页),翻页在其后。
export function filterPicksPool(nodes: Node[], tab: PicksPoolTab, keyword: string): Node[] {
  const kw = keyword.trim().toLowerCase()
  return nodes.filter((n) => {
    if (tab === 'self' && n.source !== 'self-hosted') return false
    if (tab === 'fav' && n.favorite !== true) return false
    if (!kw) return true
    return [n.name, n.display_name, n.source, n.region].some((s) =>
      (s || '').toLowerCase().includes(kw)
    )
  })
}

// paginateSlice 前端分页切片:page 从 1 起;越界页收敛到最后一页(过滤后总数
// 缩小时不落在空页)。空列表恒返回空切片。
export function paginateSlice<T>(items: T[], page: number, pageSize: number): T[] {
  const pageCount = Math.max(1, Math.ceil(items.length / pageSize))
  const p = Math.min(Math.max(1, page), pageCount)
  return items.slice((p - 1) * pageSize, p * pageSize)
}

// mergePicks 把一组 NodeKey 批量并入已选(全选当前过滤结果):按 key 去重,
// 已在选的项保留原别名,新项为无别名项。返回新数组,不改入参。
export function mergePicks(selected: NodePick[], keys: string[]): NodePick[] {
  const seen = new Set(selected.map((p) => p.key))
  const merged = [...selected]
  for (const key of keys) {
    if (!seen.has(key)) {
      seen.add(key)
      merged.push({ key })
    }
  }
  return merged
}
