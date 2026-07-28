// IP 规则区块的组件测试(pull-guard ticket 06):列表中文化呈现、新增表单接线、
// 删除、sub->global 升级,以及"每次写操作后重新拉取"。
// API 层整体打桩:线上契约由后端 handlers_iprules_test.go 守,这里只验证 UI 接线。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import IPRuleList from './IPRuleList.vue'
import * as api from '@/api/ip-rules'
import type { IPRule } from '@/api/ip-rules'

vi.mock('@/api/ip-rules', () => ({
  listIPRules: vi.fn(),
  createIPRule: vi.fn(),
  deleteIPRule: vi.fn(),
  promoteIPRule: vi.fn()
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

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
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) => h('div', { class: 'tc-row', key: i }, cellOf(row)))
      ])
  }
})
const ElTagStub = defineComponent({
  name: 'ElTag',
  props: { type: { type: String, default: '' } },
  setup(props, { slots }) {
    return () => h('span', { class: 'el-tag-stub', 'data-type': props.type }, slots.default?.())
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: { type: { type: String, default: '' } },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        { class: `el-button-stub btn-${props.type || 'default'}`, onClick: () => emit('click') },
        slots.default?.()
      )
  }
})
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: { modelValue: { type: String, default: '' } },
  setup(_, { slots }) {
    return () => h('div', { class: 'el-select-stub' }, slots.default?.())
  }
})
const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: { label: { type: String, default: '' }, value: { type: String, default: '' } },
  setup(props) {
    return () => h('div', { class: 'el-option-stub', 'data-value': props.value }, props.label)
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, [slots.header?.(), slots.default?.()])
    }
  })

const rule = (over: Partial<IPRule> = {}): IPRule => ({
  id: 1,
  ip_or_cidr: '203.0.113.10',
  scope: 'sub',
  source: 'manual',
  expires_at: '2026-07-28T08:00:00Z',
  expired: false,
  permanent: false,
  comment: '',
  created_at: '2026-07-27T08:00:00Z',
  ...over
})

interface RuleListVM {
  load: () => Promise<void>
  form: { target: string; scope: string; duration: string; comment: string }
}

const mountBlock = (rules: IPRule[] = []) => {
  vi.mocked(api.listIPRules).mockResolvedValue({ rules })
  return mount(IPRuleList, {
    global: {
      directives: { loading: {} },
      stubs: {
        'el-card': SimpleSlotStub('ElCard'),
        'el-alert': SimpleSlotStub('ElAlert'),
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-tag': ElTagStub,
        'el-button': ElButtonStub,
        'el-select': ElSelectStub,
        'el-option': ElOptionStub,
        'el-input': SimpleSlotStub('ElInput'),
        'el-empty': SimpleSlotStub('ElEmpty')
      }
    }
  })
}

const vmOf = (wrapper: ReturnType<typeof mountBlock>) => wrapper.vm as unknown as RuleListVM

describe('IPRuleList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.createIPRule).mockResolvedValue(rule())
    vi.mocked(api.deleteIPRule).mockResolvedValue({ success: true })
    vi.mocked(api.promoteIPRule).mockResolvedValue(rule({ scope: 'global' }))
  })

  it('loads the rule list on mount', async () => {
    mountBlock()
    await flushPromises()
    expect(api.listIPRules).toHaveBeenCalledTimes(1)
  })

  it('renders scope and source in Chinese with the target and comment', async () => {
    const wrapper = mountBlock([
      rule({ id: 1, ip_or_cidr: '203.0.113.10', scope: 'sub', source: 'manual', comment: '刷量' }),
      rule({ id: 2, ip_or_cidr: '198.51.100.0/24', scope: 'global', source: 'auto' })
    ])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('拉取黑名单')
    expect(text).toContain('整站拒止')
    expect(text).toContain('手动')
    expect(text).toContain('自动')
    expect(text).toContain('203.0.113.10')
    expect(text).toContain('198.51.100.0/24')
    expect(text).toContain('刷量')
  })

  it('shows 永久 for a permanent rule and flags a lapsed one', async () => {
    const wrapper = mountBlock([
      rule({ id: 1, permanent: true, expires_at: null }),
      rule({ id: 2, expired: true })
    ])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('永久')
    expect(text).toContain('已失效')
  })

  it('creates a bounded sub rule and reloads the list', async () => {
    const wrapper = mountBlock()
    await flushPromises()

    const vm = vmOf(wrapper)
    vm.form.target = ' 203.0.113.10 '
    vm.form.scope = 'sub'
    vm.form.duration = '1h'
    vm.form.comment = ' 刷量 '
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    expect(api.createIPRule).toHaveBeenCalledWith({
      ip_or_cidr: '203.0.113.10',
      scope: 'sub',
      duration: '1h',
      comment: '刷量'
    })
    // 写后重新读:规则是安全判定,不靠本地状态假装生效
    expect(api.listIPRules).toHaveBeenCalledTimes(2)
  })

  it('rejects an empty target without calling the API', async () => {
    const wrapper = mountBlock()
    await flushPromises()

    vmOf(wrapper).form.target = '   '
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    expect(api.createIPRule).not.toHaveBeenCalled()
  })

  it('clears the form after a successful create', async () => {
    const wrapper = mountBlock()
    await flushPromises()

    const vm = vmOf(wrapper)
    vm.form.target = '203.0.113.10'
    vm.form.comment = '刷量'
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    expect(vm.form.target).toBe('')
    expect(vm.form.comment).toBe('')
    // 默认档位回到最保守的一组
    expect(vm.form.scope).toBe('sub')
    expect(vm.form.duration).toBe('24h')
  })

  it('deletes a rule and reloads', async () => {
    const wrapper = mountBlock([rule({ id: 7 })])
    await flushPromises()

    await wrapper.find('.btn-danger').trigger('click')
    await flushPromises()

    expect(api.deleteIPRule).toHaveBeenCalledWith(7)
    expect(api.listIPRules).toHaveBeenCalledTimes(2)
  })

  it('offers promote only on sub rules and lifts them to global', async () => {
    const wrapper = mountBlock([rule({ id: 9, scope: 'sub' })])
    await flushPromises()
    expect(wrapper.text()).toContain('升级整站')

    await wrapper.find('.btn-warning').trigger('click')
    await flushPromises()

    expect(api.promoteIPRule).toHaveBeenCalledWith(9)
    expect(api.listIPRules).toHaveBeenCalledTimes(2)
  })

  it('hides the promote action on a rule that is already global', async () => {
    const wrapper = mountBlock([rule({ id: 3, scope: 'global' })])
    await flushPromises()

    expect(wrapper.text()).not.toContain('升级整站')
    expect(wrapper.find('.btn-warning').exists()).toBe(false)
  })

  it('offers both scopes and the duration ladder in the create form', async () => {
    const wrapper = mountBlock()
    await flushPromises()

    const labels = wrapper.findAll('.el-option-stub').map((o) => o.text())
    expect(labels).toContain('整站拒止')
    expect(labels).toContain('拉取黑名单')
    expect(labels).toContain('1 小时')
    expect(labels).toContain('24 小时')
    expect(labels).toContain('永久')
  })
})
