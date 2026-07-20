import type { Node } from '@/types'
import type { ScoreLevel } from '@/components/exam/stability'

// 节点筛选谓词:纯函数,结构化条件对象(非 UI 状态)。
// 统一节点表与将来的订阅动态查询(票据 23)同一事实源——两处都消费本模块,
// 不各自实现一套过滤语义。所有函数无副作用、不改入参、返回新值。

// NodeFilterCriteria 是筛选条件的结构化描述。null/''/空数组一律表示"该维度不筛选"。
export interface NodeFilterCriteria {
  source: string // '' 全部 | 'self-hosted' 仅自建 | 具体机场名(精确)
  region: string // '' | 国家码(大小写不敏感,精确)
  type: string // '' | 协议类型(精确)
  available: boolean | null // null 不筛选
  blocked: boolean | null
  stale: boolean | null
  keyword: string // 名称 / 标准名 / 服务器 子串模糊(大小写不敏感)
  tags: string[] // 标签:命中任一即匹配(OR)
  unlock: string[] // 解锁能力:每个目标都需已解锁(AND)
  stabilityBand: ScoreLevel | null // 稳定性分档 good/fair/poor
}

// 提供筛选所需、但不在节点自身的派生数据(如体检得出的稳定性分档)。
export interface NodeFilterContext {
  bandByKey?: Record<string, ScoreLevel | undefined> // node_key -> 稳定性分档
}

export const emptyCriteria = (): NodeFilterCriteria => ({
  source: '',
  region: '',
  type: '',
  available: null,
  blocked: null,
  stale: null,
  keyword: '',
  tags: [],
  unlock: [],
  stabilityBand: null
})

export const matchesSource = (n: Node, source: string): boolean => {
  if (!source) return true
  return n.source === source // 'self-hosted' 与具体机场名统一走精确匹配
}

export const matchesRegion = (n: Node, region: string): boolean =>
  !region || (n.region ?? '').toUpperCase() === region.toUpperCase()

export const matchesType = (n: Node, type: string): boolean => !type || n.type === type

// 三态布尔:null 不筛选,否则值需相等。
export const matchesFlag = (value: boolean, want: boolean | null): boolean =>
  want === null || value === want

export const matchesKeyword = (n: Node, keyword: string): boolean => {
  const q = keyword.trim().toLowerCase()
  if (!q) return true
  return [n.name, n.display_name, n.server].some((f) => (f ?? '').toLowerCase().includes(q))
}

// 标签:OR 语义——节点带有任一所选标签即命中(标签是分类维度)。
export const matchesTags = (n: Node, tags: string[]): boolean => {
  if (tags.length === 0) return true
  const own = n.tags ?? []
  return tags.some((t) => own.includes(t))
}

// 解锁能力:AND 语义——要求所选每个目标都已解锁(能力是"必须满足"的要求)。
export const matchesUnlock = (n: Node, targets: string[]): boolean => {
  if (targets.length === 0) return true
  const results = n.unlock_results
  if (!results) return false
  return targets.every((t) => results[t]?.available === true)
}

// 稳定性分档:分档来自体检摘要(不在节点自身),经 context 注入。
// 无已加载分档的节点在启用该筛选时视为不命中。
export const matchesStabilityBand = (
  nodeKey: string,
  band: ScoreLevel | null,
  bandByKey: Record<string, ScoreLevel | undefined>
): boolean => {
  if (!band) return true
  return bandByKey[nodeKey] === band
}

export const matchesNode = (n: Node, c: NodeFilterCriteria, ctx: NodeFilterContext = {}): boolean =>
  matchesSource(n, c.source) &&
  matchesRegion(n, c.region) &&
  matchesType(n, c.type) &&
  matchesFlag(n.available, c.available) &&
  matchesFlag(n.blocked, c.blocked) &&
  matchesFlag(n.stale, c.stale) &&
  matchesKeyword(n, c.keyword) &&
  matchesTags(n, c.tags) &&
  matchesUnlock(n, c.unlock) &&
  matchesStabilityBand(n.node_key, c.stabilityBand, ctx.bandByKey ?? {})

// filterNodes 返回命中条件的新数组(不改入参)。
export const filterNodes = (
  nodes: Node[],
  c: NodeFilterCriteria,
  ctx?: NodeFilterContext
): Node[] => nodes.filter((n) => matchesNode(n, c, ctx))

// isActiveCriteria 判断是否存在任何有效筛选(用于空态/计数提示)。
export const isActiveCriteria = (c: NodeFilterCriteria): boolean =>
  !!c.source ||
  !!c.region ||
  !!c.type ||
  c.available !== null ||
  c.blocked !== null ||
  c.stale !== null ||
  c.keyword.trim() !== '' ||
  c.tags.length > 0 ||
  c.unlock.length > 0 ||
  c.stabilityBand !== null

export type SortOrder = 'asc' | 'desc'

// sortNodes 返回排序后的新数组(不原地改)。node_key 作稳定次级键,保证确定性。
export const sortNodes = (nodes: Node[], by: string, order: SortOrder): Node[] => {
  const dir = order === 'desc' ? -1 : 1
  const primary = (a: Node, b: Node): number => {
    switch (by) {
      case 'name':
        return (a.display_name || a.name).localeCompare(b.display_name || b.name)
      case 'region':
        return (a.region ?? '').localeCompare(b.region ?? '')
      case 'source':
        return a.source.localeCompare(b.source)
      default:
        return a.latency - b.latency
    }
  }
  return [...nodes].sort((a, b) => {
    const c = primary(a, b)
    return dir * (c !== 0 ? c : a.node_key.localeCompare(b.node_key))
  })
}
