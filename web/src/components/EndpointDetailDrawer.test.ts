import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Endpoint } from '@/types'
import EndpointDetailDrawer from './EndpointDetailDrawer.vue'
import client from '@/api/client'

// Mock API client:打开抽屉只读 preview,测试/实测归 EndpointTestSection(本套件打桩)
vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

// Mock 拉取统计段:本套件只验证 endpoint-id 接线,统计呈现归 IPStatsTable 自身
vi.mock('@/components/IPStatsTable.vue', () => ({
  default: defineComponent({
    name: 'IPStatsTable',
    props: { endpointId: { type: Number, required: true } },
    setup(props) {
      return () => h('div', { class: 'ip-stats-stub', 'data-endpoint-id': props.endpointId })
    }
  })
}))

// Mock 订阅测试段:行为归 EndpointTestSection.test.ts,抽屉只验证挂载接线
vi.mock('@/components/EndpointTestSection.vue', () => ({
  default: defineComponent({
    name: 'EndpointTestSection',
    props: { endpoint: { type: Object, required: true } },
    setup() {
      return () => h('div', { class: 'test-section-stub' })
    }
  })
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

// el-table/el-table-column 测试桩:列按行渲染 scoped slot,并渲染列头 label;
// 无 default 插槽的 prop 列按 row[prop] 渲染(与真实 el-table 一致)
const ElTableStub = defineComponent({
  name: 'ElTable',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide('rows', toRef(props, 'data'))
    return () =>
      h(
        'div',
        { class: 'el-table-stub' },
        props.data.length > 0 ? slots.default?.() : slots.empty?.()
      )
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
const ElDrawerStub = defineComponent({
  name: 'ElDrawer',
  props: { modelValue: { type: Boolean, default: false }, title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'el-drawer-stub' }, [
            h('div', { class: 'drawer-title' }, props.title),
            slots.default?.()
          ])
        : null
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () => h('button', { onClick: () => emit('click') }, slots.default?.())
  }
})
const ElDescriptionsStub = defineComponent({
  name: 'ElDescriptions',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-descriptions-stub' }, slots.default?.())
  }
})
const ElDescriptionsItemStub = defineComponent({
  name: 'ElDescriptionsItem',
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'desc-item' }, [
        h('span', { class: 'desc-label' }, props.label),
        slots.default?.()
      ])
  }
})
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  }
})
const ElRadioGroupStub = defineComponent({
  name: 'ElRadioGroup',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue', 'change'],
  setup(_, { slots }) {
    return () => h('div', { class: 'el-radio-group-stub' }, slots.default?.())
  }
})
const ElRadioButtonStub = defineComponent({
  name: 'ElRadioButton',
  props: { label: { type: String, default: '' } },
  setup(props) {
    return () => h('span', { class: 'el-radio-button-stub' }, props.label)
  }
})
const ElCollapseStub = defineComponent({
  name: 'ElCollapse',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse-stub' }, slots.default?.())
  }
})
const ElCollapseItemStub = defineComponent({
  name: 'ElCollapseItem',
  props: { title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-collapse-item-stub' }, [
        h('div', { class: 'collapse-title' }, props.title),
        slots.default?.()
      ])
  }
})
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  setup(props) {
    return () => h('textarea', { class: 'el-input-stub' }, props.modelValue)
  }
})
const StatusDotStub = defineComponent({
  name: 'StatusDot',
  setup() {
    return () => h('span', { class: 'status-dot-stub' })
  }
})

const endpoint: Endpoint = {
  id: 7,
  alias: '老爸的手机',
  path: 'abc123',
  token: 'tok',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z',
  name_mode: '',
  name_template: '',
  conditions: '',
  template_name: '',
  public_name: '家里宽带',
  availability: { available: 3, total: 5 }
}

const subscriptionUrl = 'http://localhost:8080/sub/abc123?token=tok'

const previewPayload = {
  format: 'clash',
  count: 2,
  content: 'proxies:\n  - name: a',
  nodes: [
    {
      name: 'XX 香港 01',
      display_name: '🇭🇰 香港 JS-01',
      region: 'HK',
      latency: 120,
      source: '极速机场',
      available: true
    },
    {
      name: 'XX 美国 02',
      display_name: '🇺🇸 美国 JS-02',
      region: 'US',
      latency: 0,
      source: '极速机场',
      available: false
    }
  ]
}

const mountDrawer = (modelValue: boolean, ep: Endpoint = endpoint) => {
  vi.mocked(client.get).mockImplementation(async (url: unknown) => {
    const u = String(url)
    if (u.startsWith(`/endpoints/${ep.id}/preview`)) return previewPayload as never
    return {} as never
  })
  return mount(EndpointDetailDrawer, {
    props: { modelValue, endpoint: ep, subscriptionUrl },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-drawer': ElDrawerStub,
        'el-descriptions': ElDescriptionsStub,
        'el-descriptions-item': ElDescriptionsItemStub,
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub,
        'el-radio-group': ElRadioGroupStub,
        'el-radio-button': ElRadioButtonStub,
        'el-collapse': ElCollapseStub,
        'el-collapse-item': ElCollapseItemStub,
        'el-input': ElInputStub,
        StatusDot: StatusDotStub
      }
    }
  })
}

const clickButton = async (wrapper: ReturnType<typeof mountDrawer>, label: string) => {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  expect(btn, `button ${label}`).toBeDefined()
  await btn!.trigger('click')
}

describe('EndpointDetailDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('打开抽屉只拉取下发节点清单（纯读取）,不自动拉取验证、不自动实测', async () => {
    mountDrawer(true)
    await flushPromises()

    const getCalls = vi.mocked(client.get).mock.calls.map(([url]) => String(url))
    expect(getCalls).toEqual(['/endpoints/7/preview?format=clash'])
    // 无 POST:拉取验证与现场实测都是显式动作(打开不自动测)
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
  })

  it('关闭状态不拉取任何数据', async () => {
    mountDrawer(false)
    await flushPromises()
    expect(vi.mocked(client.get)).not.toHaveBeenCalled()
  })

  it('概况段展示端点信息，轻管理动作全部上抛（启停/命名设置/公开名称/节点范围/精选/删除/二维码）', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('老爸的手机')
    expect(text).toContain(subscriptionUrl)
    expect(text).toContain('跟随全局')
    expect(text).toContain('全量')
    // 公开名称(issue #38):概况段展示当前值
    expect(text).toContain('家里宽带')
    // 精选(issue #87):概况段展示当前精选状态(未配 = 全量)
    expect(text).toContain('精选节点')

    const cases: Array<[string, string]> = [
      ['禁用', 'toggle'],
      ['命名设置', 'name-config'],
      ['公开名称', 'public-name'],
      ['节点范围', 'conditions'],
      ['精选节点', 'picks'],
      ['删除', 'delete'],
      ['二维码', 'qrcode']
    ]
    for (const [label, event] of cases) {
      await clickButton(wrapper, label)
      expect(wrapper.emitted(event)).toBeTruthy()
      expect(wrapper.emitted(event)![0]).toEqual([endpoint])
    }
  })

  it('概况段精选状态：已配精选显示「精选 N 个节点」（issue #87）', async () => {
    const picked: Endpoint = {
      ...endpoint,
      node_picks: '[{"key":"hk1.example.com:443","alias":"别名"},{"key":"us1.example.com:8443"}]'
    }
    const wrapper = mountDrawer(true, picked)
    await flushPromises()
    expect(wrapper.text()).toContain('精选 2 个节点')
  })

  it('概况段订阅 URL 可复制', async () => {
    const writeText = vi.fn(async () => {})
    Object.assign(navigator, { clipboard: { writeText } })

    const wrapper = mountDrawer(true)
    await flushPromises()

    await clickButton(wrapper, '复制')
    expect(writeText).toHaveBeenCalledWith(subscriptionUrl)
  })

  it('下发节点清单段：展示名称/地区/延迟/可用/来源，原文折叠，测试段与统计段接线', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('下发节点清单')
    expect(text).toContain('共 2 个节点')
    expect(text).toContain('🇭🇰 香港 JS-01')
    expect(text).toContain('HK')
    expect(text).toContain('120ms')
    expect(text).toContain('可用')
    expect(text).toContain('不可用')
    expect(text).toContain('极速机场')
    // 订阅原文折叠区、测试段(stub)与统计段
    expect(text).toContain('订阅原文')
    expect(wrapper.find('.test-section-stub').exists()).toBe(true)
    expect(wrapper.find('.ip-stats-stub').attributes('data-endpoint-id')).toBe('7')
  })

  it('切换 Clash/V2Ray 重新拉取对应格式清单', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const radioGroup = wrapper.findComponent({ name: 'ElRadioGroup' })
    radioGroup.vm.$emit('update:modelValue', 'v2ray')
    radioGroup.vm.$emit('change', 'v2ray')
    await flushPromises()

    const getCalls = vi.mocked(client.get).mock.calls.map(([url]) => String(url))
    expect(getCalls).toContain('/endpoints/7/preview?format=v2ray')
  })
})
