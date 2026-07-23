// 实测历史:拉取全量记录,按节点聚合并附与直连基线的差值(纯计算见 ../utils.ts)。
import { computed, ref, type Ref } from 'vue'
import { deleteSpeedtestResult, listSpeedtestResults, type SpeedtestResult } from '@/api/speedtest'
import { isOrphanKey, toAggregateViews, type AggregateView } from '../utils'

// AggregateRow 表格行:聚合视图 + 标注节点是否已不在池(孤儿,UI 标"已失效")。
export interface AggregateRow extends AggregateView {
  orphan: boolean
}

// buildRows 聚合视图 + 池 key 集 → 表格行(抽纯函数便于单测孤儿标注)。
export function buildRows(
  results: readonly SpeedtestResult[],
  poolKeys: ReadonlySet<string>
): AggregateRow[] {
  return toAggregateViews(results).map((view) => ({
    ...view,
    orphan: isOrphanKey(view.nodeKey, poolKeys)
  }))
}

// useSpeedtestHistory 历史记录的加载/删除;行视图由记录与当前池 key 集派生
// (孤儿语义依赖池快照,是展示态不是入库态,节点回池后"已失效"自动消失)。
export function useSpeedtestHistory(poolKeys: Ref<ReadonlySet<string>>) {
  const records = ref<SpeedtestResult[]>([])
  const loading = ref(false)

  const load = async () => {
    loading.value = true
    try {
      records.value = await listSpeedtestResults()
    } finally {
      loading.value = false
    }
  }

  const remove = async (id: number) => {
    await deleteSpeedtestResult(id)
    await load()
  }

  const rows = computed<AggregateRow[]>(() => buildRows(records.value, poolKeys.value))

  return { records, loading, load, remove, rows }
}
