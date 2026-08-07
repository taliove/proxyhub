import { describe, it, expect } from 'vitest'
import { computed, reactive, ref } from 'vue'
import { useSelectAllFiltered } from './useSelectAllFiltered'
import { emptyCriteria } from '../predicates'
import type { UnifiedNode } from '../selfmerge'

const node = (over: Partial<UnifiedNode> = {}): UnifiedNode =>
  ({
    name: 'n',
    display_name: '',
    type: 'vmess',
    server: 'example.com',
    port: 443,
    tls: true,
    region: 'HK',
    source: 'airport-a',
    latency: 100,
    available: true,
    node_key: 'k',
    blocked: false,
    stale: false,
    ...over
  }) as UnifiedNode

const rows = (n: number): UnifiedNode[] =>
  Array.from({ length: n }, (_, i) => node({ node_key: `k${i}` }))

// 50 条筛选结果、每页 20 条的标准夹具
const setup = (totalRows = 50, pageSize = 20) => {
  const filtered = ref<UnifiedNode[]>(rows(totalRows))
  const page = ref(1)
  const pagedNodes = computed(() =>
    filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize)
  )
  const criteria = reactive(emptyCriteria())
  const saf = useSelectAllFiltered({ filtered, pagedNodes, criteria })
  const selectPage = () => saf.onSelectionChange([...pagedNodes.value])
  return { filtered, page, pagedNodes, criteria, saf, selectPage }
}

describe('useSelectAllFiltered', () => {
  it('初始：无勾选，提示条隐藏，有效选择为空', () => {
    const { saf } = setup()
    expect(saf.promptVisible.value).toBe(false)
    expect(saf.allFiltered.value).toBe(false)
    expect(saf.effectiveSelection.value).toEqual([])
  })

  it('部分勾选当前页：提示条隐藏，有效选择 = 勾选行', () => {
    const { saf, pagedNodes } = setup()
    saf.onSelectionChange(pagedNodes.value.slice(0, 3))
    expect(saf.pageFullySelected.value).toBe(false)
    expect(saf.promptVisible.value).toBe(false)
    expect(saf.effectiveSelection.value.map((n) => n.node_key)).toEqual(['k0', 'k1', 'k2'])
  })

  it('表头全选当前页且筛选结果多于一页：提示条出现', () => {
    const { saf, selectPage } = setup()
    selectPage()
    expect(saf.pageFullySelected.value).toBe(true)
    expect(saf.promptVisible.value).toBe(true)
    expect(saf.allFiltered.value).toBe(false)
  })

  it('筛选结果不超过一页时，即使整页勾选也不出提示条', () => {
    const { saf, selectPage } = setup(20, 20)
    selectPage()
    expect(saf.pageFullySelected.value).toBe(true)
    expect(saf.promptVisible.value).toBe(false)
  })

  it('进入全部筛选结果作用域：有效选择 = filtered 全集', () => {
    const { saf, selectPage } = setup()
    selectPage()
    saf.enter()
    expect(saf.allFiltered.value).toBe(true)
    expect(saf.promptVisible.value).toBe(true)
    expect(saf.effectiveSelection.value).toHaveLength(50)
  })

  it('全部筛选结果作用域下翻页不清除（勾选被表格清空，有效选择仍为全集）', () => {
    const { saf, selectPage, page } = setup()
    selectPage()
    saf.enter()
    // 翻页:el-table 对当前页重算勾选,selection-change 带回新页之前会先清空
    page.value = 2
    saf.onSelectionChange([])
    expect(saf.allFiltered.value).toBe(true)
    expect(saf.promptVisible.value).toBe(true)
    expect(saf.effectiveSelection.value).toHaveLength(50)
  })

  it('退出作用域：有效选择回退到勾选行', () => {
    const { saf, selectPage } = setup()
    selectPage()
    saf.enter()
    saf.exit()
    expect(saf.allFiltered.value).toBe(false)
    expect(saf.effectiveSelection.value).toHaveLength(20)
    // 仍是整页勾选,提示条回到"点击选中全部"态
    expect(saf.promptVisible.value).toBe(true)
  })

  it('修改筛选条件自动退出全部筛选结果作用域', () => {
    const { saf, selectPage, criteria } = setup()
    selectPage()
    saf.enter()
    expect(saf.allFiltered.value).toBe(true)
    criteria.keyword = 'hk'
    // watch 回调同步触发(flush: sync 语义由 composable 内部保证)
    expect(saf.allFiltered.value).toBe(false)
  })

  it('作用域内取消勾选行不影响有效选择（全集口径不回退）', () => {
    const { saf, selectPage, pagedNodes } = setup()
    selectPage()
    saf.enter()
    saf.onSelectionChange(pagedNodes.value.slice(0, 1))
    expect(saf.effectiveSelection.value).toHaveLength(50)
  })
})
