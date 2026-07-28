// 订阅 IP 明细的组件测试(pull-guard ticket 06):status 列中文化(含被拦记录)、
// 行内封禁写 scope=sub 规则并重新拉取、非超管不渲染操作列。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import IPStatsTable from './IPStatsTable.vue'
import client from '@/api/client'
import * as ipRules from '@/api/ip-rules'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }
}))

vi.mock('@/api/ip-rules', () => ({ createIPRule: vi.fn() }))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

interface StatRow {
  ip: string
  status: string
  count: number
  last_pull: string
  country: string
  region: string
  city: string
  isp: string
}

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
// el-select 桩带受控写回:行内时长选择要能被测试改动。
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'select',
        {
          class: 'el-select-stub',
          value: props.modelValue,
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value)
        },
        slots.default?.()
      )
  }
})
const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: { label: { type: String, default: '' }, value: { type: String, default: '' } },
  setup(props) {
    return () => h('option', { value: props.value }, props.label)
  }
})
const ElEmptyStub = defineComponent({
  name: 'ElEmpty',
  setup() {
    return () => h('div', { class: 'el-empty-stub' })
  }
})

const stat = (over: Partial<StatRow> = {}): StatRow => ({
  ip: '203.0.113.10',
  status: 'ok',
  count: 3,
  last_pull: '2026-07-27T08:00:00Z',
  country: '中国',
  region: '广东',
  city: '广州',
  isp: '电信',
  ...over
})

const mountTable = (rows: StatRow[] = [], superAdmin = true) => {
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.setAuth('root', superAdmin ? 'super_admin' : 'user')
  vi.mocked(client.get).mockResolvedValue(rows as never)
  return mount(IPStatsTable, {
    props: { endpointId: 42 },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-tag': ElTagStub,
        'el-button': ElButtonStub,
        'el-select': ElSelectStub,
        'el-option': ElOptionStub,
        'el-empty': ElEmptyStub
      }
    }
  })
}

describe('IPStatsTable', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(ipRules.createIPRule).mockResolvedValue({
      id: 1,
      ip_or_cidr: '203.0.113.10',
      scope: 'sub',
      source: 'manual',
      expires_at: null,
      expired: false,
      permanent: true,
      comment: '',
      created_at: '2026-07-27T08:00:00Z'
    })
  })

  it('loads the endpoint stats on mount', async () => {
    mountTable()
    await flushPromises()
    expect(client.get).toHaveBeenCalledWith('/endpoints/42/stats')
  })

  it('renders every pull status in Chinese, including blocked attempts', async () => {
    const wrapper = mountTable([
      stat({ ip: '203.0.113.1', status: 'ok' }),
      stat({ ip: '203.0.113.2', status: 'rate_limited' }),
      stat({ ip: '203.0.113.3', status: 'geo_blocked' }),
      stat({ ip: '203.0.113.4', status: 'geo_would_block' }),
      stat({ ip: '203.0.113.5', status: 'blacklisted' }),
      stat({ ip: '203.0.113.6', status: 'disabled' }),
      stat({ ip: '203.0.113.7', status: 'bad_token' })
    ])
    await flushPromises()

    const text = wrapper.text()
    for (const label of ['成功', '限频', '地域拦截', '地域观察', '黑名单', '已禁用', '错误令牌']) {
      expect(text).toContain(label)
    }
    // 状态列有表头,而不是只在某个 tooltip 里
    expect(wrapper.findAll('.tc-label').map((l) => l.text())).toContain('状态')
    // 英文原值不外泄
    expect(text).not.toContain('geo_would_block')
  })

  it('bans the row IP as a sub-scope rule and reloads the stats', async () => {
    const wrapper = mountTable([stat({ ip: '198.51.100.7', status: 'rate_limited' })])
    await flushPromises()

    await wrapper.find('.btn-danger').trigger('click')
    await flushPromises()

    expect(ipRules.createIPRule).toHaveBeenCalledWith(
      expect.objectContaining({
        ip_or_cidr: '198.51.100.7',
        scope: 'sub',
        duration: '24h'
      })
    )
    // 写后重新读:让操作者看到后续拉取以 blacklisted 落账
    expect(client.get).toHaveBeenCalledTimes(2)
  })

  it('honours the per-row duration selection when banning', async () => {
    const wrapper = mountTable([stat({ ip: '198.51.100.8' })])
    await flushPromises()

    const select = wrapper.find('select.el-select-stub')
    await select.setValue('1h')
    await wrapper.find('.btn-danger').trigger('click')
    await flushPromises()

    expect(ipRules.createIPRule).toHaveBeenCalledWith(
      expect.objectContaining({ ip_or_cidr: '198.51.100.8', duration: '1h' })
    )
  })

  it('offers the same duration ladder as the audit ban drawer', async () => {
    const wrapper = mountTable([stat()])
    await flushPromises()

    expect(wrapper.findAll('option').map((o) => o.text())).toEqual(['1 小时', '24 小时', '永久'])
  })

  it('hides the ban column for a non super admin (the endpoint is adminGuard)', async () => {
    const wrapper = mountTable([stat()], false)
    await flushPromises()

    expect(wrapper.findAll('.tc-label').map((l) => l.text())).not.toContain('操作')
    expect(wrapper.find('.btn-danger').exists()).toBe(false)
    // 明细本身仍然可见
    expect(wrapper.text()).toContain('成功')
  })

  it('keeps the empty state when there are no pulls', async () => {
    const wrapper = mountTable([])
    await flushPromises()
    expect(wrapper.find('.el-empty-stub').exists()).toBe(true)
  })
})
