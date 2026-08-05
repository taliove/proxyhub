import { computed, ref, watch, type Ref } from 'vue'
import type { NodeFilterCriteria } from '../predicates'
import type { UnifiedNode } from '../selfmerge'

// useSelectAllFiltered 管理节点表的"选中全部筛选结果"作用域(issue #52,Gmail 式提示条)。
//
// 表格勾选(selection)永远只是当前页子集;表头全选也只覆盖当前页。当整页被勾选且筛选
// 结果多于一页时,装配层显示提示条,点击 enter() 进入 allFiltered 作用域:有效选择
// (effectiveSelection)切换为 filtered 全集,翻页/改页大小不清除(el-table 翻页会清空
// 勾选,但作用域不依赖勾选态)。修改筛选条件或显式 exit() 时退出,回退到勾选行口径。
//
// 批量操作一律以 effectiveSelection 为唯一作用域;屏蔽类操作在其上再过滤机场节点
// (见 useNodeBatch 的 selectableSelection)。
export function useSelectAllFiltered(opts: {
  filtered: Ref<UnifiedNode[]>
  pagedNodes: Ref<UnifiedNode[]>
  criteria: NodeFilterCriteria
}) {
  const { filtered, pagedNodes, criteria } = opts

  // 表格勾选行(当前页子集),由 el-table selection-change 回写。
  const selection = ref<UnifiedNode[]>([])
  const allFiltered = ref(false)

  const onSelectionChange = (rows: UnifiedNode[]) => {
    selection.value = rows
  }

  // 当前页被整页勾选(表头全选,或逐行点满)。
  const pageFullySelected = computed(
    () => pagedNodes.value.length > 0 && selection.value.length === pagedNodes.value.length
  )

  // 提示条可见性:整页勾选且筛选结果多于一页时出现;进入作用域后常驻(转为"取消"文案)。
  const promptVisible = computed(
    () =>
      allFiltered.value ||
      (pageFullySelected.value && filtered.value.length > pagedNodes.value.length)
  )

  // 有效选择:批量操作作用域。全集口径优先,不随勾选态回退。
  const effectiveSelection = computed<UnifiedNode[]>(() =>
    allFiltered.value ? filtered.value : selection.value
  )

  const enter = () => {
    allFiltered.value = true
  }
  const exit = () => {
    allFiltered.value = false
  }

  // 筛选条件变化立即退出作用域(旧作用域基于旧条件,沿用会误伤新结果集)。
  // flush: sync 保证条件变更当拍失效,不留一帧的旧口径。
  watch(
    () => ({ ...criteria }),
    () => exit(),
    { deep: true, flush: 'sync' }
  )

  return {
    selection,
    allFiltered,
    pageFullySelected,
    promptVisible,
    effectiveSelection,
    onSelectionChange,
    enter,
    exit
  }
}
