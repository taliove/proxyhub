import { onMounted, ref } from 'vue'
import client from '@/api/client'
import type { ExamReport } from '@/types'
import { calculateExamScore, type ExamScoreResult } from '@/components/exam/score'

// 后端 /api/dashboard/top-nodes 响应条目(契约见 internal/server/handlers_dashboard.go)。
// type 由后端从节点池带出,分享操作门控(canGenerateShareLink)直接消费,无需再与节点池 join。
export interface TopNodeEntry {
  node_key: string
  report: ExamReport
  tags: string[]
  type: string
  region: string
  source: string
  available: boolean
}

// 榜单条目:聚合数据 + 算分结果。
export interface TopNodeItem extends TopNodeEntry {
  score: ExamScoreResult
}

// 榜单条数(数值已定稿 Top 10,见 spec-homepage-dashboard)。
export const TOP_NODES_LIMIT = 10

// rankTopNodes 算分并排序取 Top N:总分降序;并列时稳定性分高者在前(缺段按 -1 垫底);
// 再并列保持接口顺序(稳定排序)。纯函数,不修改入参。
export function rankTopNodes(
  entries: readonly TopNodeEntry[],
  limit: number = TOP_NODES_LIMIT
): TopNodeItem[] {
  return entries
    .map((entry) => ({
      ...entry,
      score: calculateExamScore(entry.report)
    }))
    .sort(
      (a, b) =>
        b.score.total - a.score.total ||
        (b.score.breakdown.stability.score ?? -1) - (a.score.breakdown.stability.score ?? -1)
    )
    .slice(0, limit)
}

// 解锁类标签词表(对齐 internal/nodetag/derive.go),用于展示优先级排序。
const UNLOCK_TAGS: ReadonlySet<string> = new Set([
  'nf-full',
  'nf-originals',
  'yt-premium',
  'disney-plus',
  'openai',
  'claude',
  'gemini'
])

// 标签展示优先级:fast > 解锁类 > stable-* > 其他。
const tagPriority = (tag: string): number => {
  if (tag === 'fast') return 0
  if (UNLOCK_TAGS.has(tag)) return 1
  if (tag.startsWith('stable-')) return 2
  return 3
}

// prioritizeTags 按展示优先级重排标签;同级保持原顺序(稳定排序)。纯函数,不修改入参。
export function prioritizeTags(tags: readonly string[]): string[] {
  return tags
    .map((tag, index) => ({ tag, index }))
    .sort((a, b) => tagPriority(a.tag) - tagPriority(b.tag) || a.index - b.index)
    .map(({ tag }) => tag)
}

// useTopNodes 拉取优质节点榜单:聚合接口一次拉取后前端算分排序。
// 失败时全局拦截器已提示,此处标记 failed 供组件降级。
export function useTopNodes() {
  const items = ref<TopNodeItem[]>([])
  const loading = ref(true)
  const failed = ref(false)

  onMounted(async () => {
    try {
      const entries = await client.get<unknown, TopNodeEntry[]>('/dashboard/top-nodes')
      items.value = rankTopNodes(entries ?? [])
    } catch {
      failed.value = true
      items.value = []
    } finally {
      loading.value = false
    }
  })

  return { items, loading, failed }
}
