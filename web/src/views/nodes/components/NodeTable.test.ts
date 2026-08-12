// issue #82:行内屏蔽/解除屏蔽按钮的渲染与事件路径——机场节点按 blocked 态切换
// 「屏蔽」/「取消屏蔽」并向上 emit;自建节点(屏蔽豁免)不渲染该按钮。
// el-table 系列用 provide/inject 行数据桩(沿用 IPStatsTable.test.ts 的既有模式)。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import NodeTable from './NodeTable.vue'
import { SELF_HOSTED } from '../utils'
import type { UnifiedNode } from '../selfmerge'

const ElTableStub = defineComponent({
  name: 'ElTable',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide('rows', toRef(props, 'data'))
    return () => h('div', { class: 'el-table-stub' }, slots.default?.())
  }
})
const ElTableColumnStub = defineComponent({
  name: 'ElTableColumn',
  props: { label: { type: String, default: '' }, prop: { type: String, default: '' } },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    const cellOf = (row: unknown) =>
      slots.default
        ? slots.default({ row })
        : String((row as Record<string, unknown>)[props.prop] ?? '')
    return () =>
      h('div', { class: 'el-column-stub', 'data-label': props.label }, [
        ...rows.value.map((row, i) => h('div', { class: 'tc-row', key: i }, cellOf(row)))
      ])
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () =>
      h('button', { class: 'el-button-stub', onClick: () => emit('click') }, slots.default?.())
  }
})
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  }
})
const passthrough = (name: string, className: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: className }, slots.default?.())
    }
  })
// el-dropdown 桩(与 AirportDetailDrawer.test 同一模式):菜单项内联渲染为 button,
// 点击经 provide/inject 派发 command,等价真实下拉。
const ElDropdownStub = defineComponent({
  name: 'ElDropdown',
  emits: ['command'],
  setup(_, { slots, emit }) {
    provide('dropdown-command', (cmd: unknown) => emit('command', cmd))
    return () => h('div', { class: 'el-dropdown-stub' }, [slots.default?.(), slots.dropdown?.()])
  }
})
const ElDropdownItemStub = defineComponent({
  name: 'ElDropdownItem',
  props: { command: { type: [String, Number, Boolean], default: undefined } },
  setup(props, { slots }) {
    const fire = inject<(cmd: unknown) => void>('dropdown-command')!
    return () =>
      h(
        'button',
        { class: 'el-dropdown-item-stub', onClick: () => fire(props.command) },
        slots.default?.()
      )
  }
})

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

const mountTable = (nodes: UnifiedNode[]) =>
  mount(NodeTable, {
    props: { nodes, loading: false, detecting: false, page: 1, pageSize: 20, total: nodes.length },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub,
        'el-popover': passthrough('ElPopover', 'el-popover-stub'),
        'el-tooltip': passthrough('ElTooltip', 'el-tooltip-stub'),
        'el-icon': passthrough('ElIcon', 'el-icon-stub'),
        'el-dropdown': ElDropdownStub,
        'el-dropdown-menu': passthrough('ElDropdownMenu', 'el-dropdown-menu-stub'),
        'el-dropdown-item': ElDropdownItemStub,
        'el-pagination': passthrough('ElPagination', 'el-pagination-stub'),
        StatusDot: passthrough('StatusDot', 'status-dot-stub'),
        NodeTestCell: passthrough('NodeTestCell', 'node-test-cell-stub')
      }
    }
  })

// 操作列第 index 行的按钮文案
const opsButtonTexts = (wrapper: ReturnType<typeof mountTable>, index: number) => {
  const col = wrapper.find('.el-column-stub[data-label="操作"]')
  expect(col.exists()).toBe(true)
  return col
    .findAll('.tc-row')
    [index].findAll('button')
    .map((b) => b.text())
}

describe('NodeTable 行内屏蔽/解除屏蔽（issue #82）', () => {
  it('未屏蔽机场节点行内显示「屏蔽」，点击 emit block 带该行', async () => {
    const row = node({ node_key: 'k-block' })
    const wrapper = mountTable([row])

    expect(opsButtonTexts(wrapper, 0)).toContain('屏蔽')
    expect(opsButtonTexts(wrapper, 0)).not.toContain('取消屏蔽')

    const btn = wrapper
      .find('.el-column-stub[data-label="操作"]')
      .findAll('button')
      .find((b) => b.text() === '屏蔽')!
    await btn.trigger('click')

    expect(wrapper.emitted('block')).toHaveLength(1)
    expect(wrapper.emitted('block')![0]).toEqual([row])
    expect(wrapper.emitted('unblock')).toBeUndefined()
  })

  it('已屏蔽机场节点行内显示「取消屏蔽」，点击 emit unblock 带该行', async () => {
    const row = node({ node_key: 'k-unblock', blocked: true })
    const wrapper = mountTable([row])

    expect(opsButtonTexts(wrapper, 0)).toContain('取消屏蔽')
    expect(opsButtonTexts(wrapper, 0)).not.toContain('屏蔽')

    const btn = wrapper
      .find('.el-column-stub[data-label="操作"]')
      .findAll('button')
      .find((b) => b.text() === '取消屏蔽')!
    await btn.trigger('click')

    expect(wrapper.emitted('unblock')).toHaveLength(1)
    expect(wrapper.emitted('unblock')![0]).toEqual([row])
    expect(wrapper.emitted('block')).toBeUndefined()
  })

  it('自建节点（屏蔽豁免）不渲染屏蔽/取消屏蔽按钮', () => {
    const row = node({ node_key: 'k-self', source: SELF_HOSTED, self_node_id: 7, enabled: true })
    const wrapper = mountTable([row])

    const texts = opsButtonTexts(wrapper, 0)
    expect(texts).not.toContain('屏蔽')
    expect(texts).not.toContain('取消屏蔽')
    // 密度收敛后行布局统一为「详情 | 编辑 | 更多▾(命名/刷新名称/启停/删除)」(critique P1)
    expect(texts).toEqual(['详情', '编辑', '更多', '命名', '刷新名称', '禁用', '删除'])
  })

  it('混合列表中屏蔽按钮只出现在机场行', () => {
    const wrapper = mountTable([
      node({ node_key: 'k-airport' }),
      node({ node_key: 'k-self', source: SELF_HOSTED, self_node_id: 7, enabled: true })
    ])

    expect(opsButtonTexts(wrapper, 0)).toContain('屏蔽')
    expect(opsButtonTexts(wrapper, 1)).not.toContain('屏蔽')
  })

  it('机场行与自建行都渲染「命名」槽位入口,点击 emit assign-slot(issue #98)', async () => {
    const wrapper = mountTable([
      node({ node_key: 'k-airport' }),
      node({ node_key: 'k-self', source: SELF_HOSTED, self_node_id: 7, enabled: true })
    ])

    expect(opsButtonTexts(wrapper, 0)).toContain('命名')
    expect(opsButtonTexts(wrapper, 1)).toContain('命名')

    const btn = wrapper
      .find('.el-column-stub[data-label="操作"]')
      .findAll('button')
      .find((b) => b.text() === '命名')!
    await btn.trigger('click')
    expect(wrapper.emitted('assign-slot')).toHaveLength(1)
  })
})
