import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Airport } from '@/types'
import { getAirportQRContent } from './airport-utils'
import Airports from './Airports.vue'
import client from '@/api/client'

// Mock API client:列表页只读 /api/airports
vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))
vi.mock('@/api/jobs', () => ({ getJob: vi.fn() }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ query: {} })
}))

describe('Airports QR Code', () => {
  describe('getAirportQRContent', () => {
    it('should extract subscription URL from airport', () => {
      const airport: Airport = {
        id: 1,
        name: 'Test Airport',
        url: 'https://example.com/subscribe/token123',
        abbr: 'TA',
        enabled: true,
        created_at: '2026-01-01T00:00:00Z'
      }

      const content = getAirportQRContent(airport)
      expect(content).toBe('https://example.com/subscribe/token123')
    })

    it('should handle airport with empty abbr', () => {
      const airport: Airport = {
        id: 2,
        name: 'Another Airport',
        url: 'https://another.example.com/sub/abc',
        abbr: '',
        enabled: false,
        created_at: '2026-01-01T00:00:00Z'
      }

      const content = getAirportQRContent(airport)
      expect(content).toBe('https://another.example.com/sub/abc')
    })
  })
})

// ---------- 行内按钮收敛(ticket 0036):只留「详情」+「刷新」,其余动作进详情抽屉 ----------

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
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    return () =>
      h('div', { class: 'el-column-stub', 'data-label': props.label }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) =>
          h('div', { class: 'tc-row', key: i }, slots.default?.({ row }))
        )
      ])
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () => h('button', { onClick: () => emit('click') }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })
// el-dropdown 桩:trigger 与 dropdown 两插槽都渲染;item 点击经 provide 上抛 command
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
  props: { command: { type: [Boolean, String, Number], default: '' } },
  setup(props, { slots }) {
    const send = inject<(cmd: unknown) => void>('dropdown-command')!
    return () =>
      h(
        'button',
        { class: 'el-dropdown-item-stub', onClick: () => send(props.command) },
        slots.default?.()
      )
  }
})
// 对话框/抽屉类:关闭态不渲染内容(modelValue=false 时)
const ModelStub = (name: string) =>
  defineComponent({
    name,
    props: {
      modelValue: { type: Boolean, default: false },
      airport: { type: Object, default: null }
    },
    emits: ['update:modelValue', 'run-test'],
    setup(props, { slots }) {
      return () =>
        props.modelValue ? h('div', { class: `${name}-stub` }, slots.default?.()) : null
    }
  })
// 运行模式测试对话框:记录 start 调用,不在打开时自动跑(ticket 0037)
const testDialogStart = vi.fn()
const AirportTestDialogStub = defineComponent({
  name: 'AirportTestDialog',
  emits: ['finished'],
  setup(_, { expose }) {
    expose({ start: testDialogStart })
    return () => h('div', { class: 'AirportTestDialog-stub' })
  }
})

describe('Airports 行内操作收敛', () => {
  const airport: Airport = {
    id: 1,
    name: '极速机场',
    url: 'https://example.com/sub/token123',
    abbr: 'JS',
    enabled: true,
    created_at: '2026-01-01T00:00:00Z'
  }

  const mountView = (rows: Airport[] = [airport]) => {
    vi.mocked(client.get).mockImplementation(async (url: unknown) => {
      if (url === '/airports') return rows
      return {}
    })
    return mount(Airports, {
      global: {
        directives: { loading: {} },
        stubs: {
          PageHeader: SimpleSlotStub('PageHeader'),
          StatusDot: SimpleSlotStub('StatusDot'),
          QRCodeDialog: ModelStub('QRCodeDialog'),
          AirportTestDialog: AirportTestDialogStub,
          AirportDetailDrawer: ModelStub('AirportDetailDrawer'),
          'el-table': ElTableStub,
          'el-table-column': ElTableColumnStub,
          'el-button': ElButtonStub,
          'el-dropdown': ElDropdownStub,
          'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
          'el-dropdown-item': ElDropdownItemStub,
          'el-tag': SimpleSlotStub('ElTag'),
          'el-icon': SimpleSlotStub('ElIcon'),
          'el-card': SimpleSlotStub('ElCard'),
          'el-dialog': ModelStub('ElDialog'),
          'el-form': SimpleSlotStub('ElForm'),
          'el-form-item': SimpleSlotStub('ElFormItem'),
          'el-input': SimpleSlotStub('ElInput')
        }
      }
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('行内操作列：详情/刷新 + 「测试」下拉（抽样测试/测全部两项）', async () => {
    const wrapper = mountView()
    await flushPromises()

    const opsColumn = wrapper.find('.el-column-stub[data-label="操作"]')
    expect(opsColumn.exists()).toBe(true)
    const buttons = opsColumn.findAll('button').map((b) => b.text())
    expect(buttons).toEqual(['详情', '刷新', '测试', '抽样测试', '测全部'])
  })

  it('行内「测试」下拉：抽样测试以 full=false 发起，测全部以 full=true 发起', async () => {
    const wrapper = mountView()
    await flushPromises()

    const opsColumn = wrapper.find('.el-column-stub[data-label="操作"]')
    const items = opsColumn.findAll('.el-dropdown-item-stub')
    expect(items).toHaveLength(2)

    await items[0].trigger('click')
    expect(testDialogStart).toHaveBeenNthCalledWith(1, airport, false)

    await items[1].trigger('click')
    expect(testDialogStart).toHaveBeenNthCalledWith(2, airport, true)
  })

  it('禁用机场行内也可测（对齐 0037 已放开的语义）', async () => {
    const disabledAirport: Airport = { ...airport, enabled: false }
    const wrapper = mountView([disabledAirport])
    await flushPromises()

    const opsColumn = wrapper.find('.el-column-stub[data-label="操作"]')
    const sampleItem = opsColumn
      .findAll('.el-dropdown-item-stub')
      .find((b) => b.text() === '抽样测试')
    await sampleItem!.trigger('click')

    expect(testDialogStart).toHaveBeenCalledWith(disabledAirport, false)
  })

  it('点「详情」打开机场详情抽屉并传入当前机场', async () => {
    const wrapper = mountView()
    await flushPromises()

    const opsColumn = wrapper.find('.el-column-stub[data-label="操作"]')
    const detailBtn = opsColumn.findAll('button').find((b) => b.text() === '详情')
    await detailBtn!.trigger('click')

    const drawer = wrapper.findComponent({ name: 'AirportDetailDrawer' })
    expect(drawer.props('modelValue')).toBe(true)
    expect(drawer.props('airport')).toEqual(airport)
  })

  it('点最近测试分数 = 查看报告：打开抽屉，不发 POST test,不调对话框 start', async () => {
    const testedAirport: Airport = {
      ...airport,
      last_test_score: 88.5,
      last_test_at: '2026-07-22T08:00:00Z',
      last_test_status: 'completed'
    }
    vi.mocked(client.get).mockImplementation(async (url: unknown) => {
      if (url === '/airports') return [testedAirport]
      return {}
    })
    const wrapper = mount(Airports, {
      global: {
        directives: { loading: {} },
        stubs: {
          PageHeader: SimpleSlotStub('PageHeader'),
          StatusDot: SimpleSlotStub('StatusDot'),
          QRCodeDialog: ModelStub('QRCodeDialog'),
          AirportTestDialog: AirportTestDialogStub,
          AirportDetailDrawer: ModelStub('AirportDetailDrawer'),
          'el-table': ElTableStub,
          'el-table-column': ElTableColumnStub,
          'el-button': ElButtonStub,
          'el-dropdown': ElDropdownStub,
          'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
          'el-dropdown-item': ElDropdownItemStub,
          'el-tag': SimpleSlotStub('ElTag'),
          'el-icon': SimpleSlotStub('ElIcon'),
          'el-card': SimpleSlotStub('ElCard'),
          'el-dialog': ModelStub('ElDialog'),
          'el-form': SimpleSlotStub('ElForm'),
          'el-form-item': SimpleSlotStub('ElFormItem'),
          'el-input': SimpleSlotStub('ElInput')
        }
      }
    })
    await flushPromises()

    const scoreCell = wrapper.find('.score-text')
    expect(scoreCell.exists()).toBe(true)
    await scoreCell.trigger('click')
    await flushPromises()

    const drawer = wrapper.findComponent({ name: 'AirportDetailDrawer' })
    expect(drawer.props('modelValue')).toBe(true)
    expect(drawer.props('airport')).toEqual(testedAirport)
    // 查看不重跑:不触发运行对话框,也没有任何 POST
    expect(testDialogStart).not.toHaveBeenCalled()
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
  })

  it('抽屉上抛 run-test 时经对话框 start 显式运行（抽样/全量）', async () => {
    const wrapper = mountView()
    await flushPromises()

    const drawer = wrapper.findComponent({ name: 'AirportDetailDrawer' })
    drawer.vm.$emit('run-test', { airport, full: false })
    drawer.vm.$emit('run-test', { airport, full: true })

    expect(testDialogStart).toHaveBeenNthCalledWith(1, airport, false)
    expect(testDialogStart).toHaveBeenNthCalledWith(2, airport, true)
  })
})
