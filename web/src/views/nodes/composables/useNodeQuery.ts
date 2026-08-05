import { computed, reactive, watch, type Ref } from 'vue'
import {
  emptyCriteria,
  filterNodes,
  sortNodes,
  type NodeFilterCriteria,
  type NodeFilterContext,
  type SortOrder
} from '../predicates'
import { scoreLevel, type ScoreLevel } from '@/components/exam/stability'
import type { UnifiedNode } from '../selfmerge'

// useNodeQuery 在客户端对统一行集执行 筛选 → 排序 → 分页,全部经 predicates.ts。
// 状态是结构化条件对象(criteria),与将来的订阅动态查询共享同一谓词模块(票据 23)。
//
// 稳定性分档筛选(票据 54)依赖体检派生的稳定性分——后端已在 /nodes 视图直接透出
// 每节点 stability_score(思路同标签票据 21),故本层直接由行集派生分档,无需按页拉体检、
// 也就没有"过滤→分页→取体检→回填→再过滤"的反馈环。无分节点不进 bandByKey,谓词按不命中处理。
export function useNodeQuery(rows: Ref<UnifiedNode[]>) {
  const criteria = reactive<NodeFilterCriteria>(emptyCriteria())
  const sort = reactive<{ by: string; order: SortOrder }>({ by: 'latency', order: 'asc' })
  const pagination = reactive({ page: 1, pageSize: 20 })

  // 由节点自带的 stability_score 派生分档(node_key -> good/fair/poor);无分节点不出现。
  const filterContext = computed<NodeFilterContext>(() => {
    const bandByKey: Record<string, ScoreLevel | undefined> = {}
    for (const n of rows.value) {
      if (typeof n.stability_score === 'number') {
        bandByKey[n.node_key] = scoreLevel(n.stability_score)
      }
    }
    return { bandByKey }
  })

  const filtered = computed<UnifiedNode[]>(
    () => filterNodes(rows.value, criteria, filterContext.value) as UnifiedNode[]
  )
  const sorted = computed<UnifiedNode[]>(
    () => sortNodes(filtered.value, sort.by, sort.order) as UnifiedNode[]
  )
  const total = computed(() => sorted.value.length)
  const pagedNodes = computed<UnifiedNode[]>(() => {
    const start = (pagination.page - 1) * pagination.pageSize
    return sorted.value.slice(start, start + pagination.pageSize)
  })
  const filteredKeys = computed(() => filtered.value.map((n) => n.node_key))

  // 任一筛选变化回到第 1 页;结果收缩导致当前页越界时纠偏。
  watch(
    () => ({ ...criteria }),
    () => {
      pagination.page = 1
    },
    { deep: true }
  )
  watch(total, (t) => {
    const maxPage = Math.max(1, Math.ceil(t / pagination.pageSize))
    if (pagination.page > maxPage) pagination.page = maxPage
  })

  const onSortChange = ({ prop, order }: { prop: string; order: string | null }) => {
    if (!order) {
      sort.by = 'latency'
      sort.order = 'asc'
    } else {
      sort.by = prop
      sort.order = order === 'ascending' ? 'asc' : 'desc'
    }
    pagination.page = 1
  }
  const setPage = (p: number) => {
    pagination.page = p
  }
  const setPageSize = (s: number) => {
    pagination.pageSize = s
    pagination.page = 1
  }

  return {
    criteria,
    sort,
    pagination,
    total,
    filtered,
    pagedNodes,
    filteredKeys,
    onSortChange,
    setPage,
    setPageSize
  }
}
