import { ref, watch, type Ref } from 'vue'
import type { Node } from '@/types'
import { fetchExamLatest } from '@/api/exam'
import { examBadgeSummary, type ExamBadge } from '@/components/exam/examhistory'

// useExamSummaries 为当前页节点批量拉取“最近一次体检”摘要,供节点行徽标使用。
// 无批量接口(后端仅单节点 latest),故按当前页节点数并发拉取;翻页/筛选换页即整批重取。
// 过期批次用递增 token 丢弃,避免快速翻页时旧响应覆盖新页。
export function useExamSummaries(nodes: Ref<Node[]>) {
  const summaries = ref<Record<string, ExamBadge>>({})
  let token = 0

  const load = async (list: Node[]) => {
    const mine = ++token
    if (list.length === 0) {
      summaries.value = {}
      return
    }
    const keys = list.map((n) => n.node_key)
    const results = await Promise.all(
      keys.map(async (key) => {
        try {
          return [key, await fetchExamLatest({ node_key: key })] as const
        } catch {
          return [key, null] as const // 单节点查询失败不拖垮整批
        }
      })
    )
    if (mine !== token) return // 已有更新的批次,丢弃过期结果

    const next: Record<string, ExamBadge> = {}
    for (const [key, entry] of results) {
      const badge = examBadgeSummary(entry)
      if (badge) next[key] = badge // 无历史不写入 -> 行内不占位
    }
    summaries.value = next
  }

  watch(nodes, (list) => load(list), { immediate: true })

  return { summaries, reload: () => load(nodes.value) }
}
