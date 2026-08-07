// 订阅地址精选节点(issue #80,spec #70)的纯函数,供选择器对话框与列表标签复用。

// parseNodePicks 把 endpoint.node_picks 原始 JSON 串解析为 NodeKey 列表。
// 空串/缺省/非法 JSON 均按空集(与后端 endpointNodePicks 降级语义一致:宁可全量,
// 不把订阅打挂);非字符串元素被过滤。
export function parseNodePicks(raw?: string): string[] {
  if (!raw) return []
  try {
    const arr: unknown = JSON.parse(raw)
    return Array.isArray(arr) ? arr.filter((x): x is string => typeof x === 'string') : []
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
