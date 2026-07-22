import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Endpoint } from '@/types'
import Endpoints from './Endpoints.vue'
import client from '@/api/client'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  ElMessageBox: { confirm: vi.fn(async () => 'confirm') }
}))

// 详情抽屉桩:记录 props 与上抛事件转发,不加载四段内容
const DrawerStub = defineComponent({
  name: 'EndpointDetailDrawer',
  props: {
    modelValue: { type: Boolean, default: false },
    endpoint: { type: Object, default: null },
    subscriptionUrl: { type: String, default: '' }
  },
  emits: ['update:modelValue', 'toggle', 'name-config', 'conditions', 'delete', 'qrcode'],
  setup(props, { emit }) {
    return () =>
      props.modelValue
        ? h(
            'div',
            {
              class: 'endpoint-drawer-stub',
              'data-endpoint-id': (props.endpoint as Endpoint | null)?.id
            },
            [
              h(
                'button',
                { class: 'drawer-toggle-btn', onClick: () => emit('toggle', props.endpoint) },
                '抽屉启停'
              ),
              h(
                'button',
                { class: 'drawer-delete-btn', onClick: () => emit('delete', props.endpoint) },
                '抽屉删除'
              )
            ]
          )
        : null
  }
})

// el-table/el-table-column 测试桩:列按行渲染 scoped slot,并渲染列头 label
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
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) =>
          h('div', { class: 'tc-row', key: i }, slots.default?.({ row }))
        )
      ])
  }
})
// el-input 桩:渲染值与 append 槽(URL 列复制/二维码按钮在 append 槽内)
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { value: { type: String, default: '' }, modelValue: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-input-stub' }, [
        h('span', { class: 'input-value' }, props.value || props.modelValue),
        slots.append?.(),
        slots.default?.()
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
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, [slots.default?.(), slots.footer?.()])
    }
  })
// 对话框类桩:关闭态不渲染(避免新建/命名对话框文案污染行内断言)
const ClosedDialogStub = (name: string) =>
  defineComponent({
    name,
    props: { modelValue: { type: Boolean, default: false } },
    setup(props, { slots }) {
      return () =>
        props.modelValue
          ? h('div', { class: `${name}-stub` }, [slots.default?.(), slots.footer?.()])
          : null
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
  availability: { available: 3, total: 5 }
}

const mountView = (list: Endpoint[] = [endpoint]) => {
  vi.mocked(client.get).mockResolvedValue(list as never)
  return mount(Endpoints, {
    global: {
      directives: { loading: {} },
      stubs: {
        PageHeader: SimpleSlotStub('PageHeader'),
        'el-card': SimpleSlotStub('ElCard'),
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub,
        'el-dialog': ClosedDialogStub('ElDialog'),
        'el-form': SimpleSlotStub('ElForm'),
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-radio-group': SimpleSlotStub('ElRadioGroup'),
        'el-radio-button': SimpleSlotStub('ElRadioButton'),
        EndpointConditionsDialog: ClosedDialogStub('EndpointConditionsDialog'),
        EndpointDetailDrawer: DrawerStub,
        QRCodeDialog: ClosedDialogStub('QRCodeDialog')
      }
    }
  })
}

describe('Endpoints(行内极简 + 详情抽屉)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('行内操作只留「详情」:无预览/统计/更多入口', async () => {
    const wrapper = mountView()
    await flushPromises()

    const rowButtons = wrapper.findAll('.el-table-stub button').map((b) => b.text())
    expect(rowButtons).toContain('详情')
    // URL 列复制/二维码仍在
    expect(rowButtons).toContain('复制')
    expect(rowButtons).toContain('二维码')
    // 预览/统计/更多下拉已收敛进抽屉
    expect(rowButtons).not.toContain('预览')
    expect(rowButtons).not.toContain('统计')
    expect(rowButtons.find((t) => t.includes('更多'))).toBeUndefined()
  })

  it('「可用」列展示 availability(可用 x/y)', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('可用')
    expect(text).toContain('3/5')
  })

  it('availability 缺失时可用列降级为 -', async () => {
    const noAvailability = { ...endpoint, availability: undefined } as Endpoint
    const wrapper = mountView([noAvailability])
    await flushPromises()
    expect(wrapper.text()).not.toContain('3/5')
  })

  it('点「详情」打开抽屉并传入端点与订阅 URL', async () => {
    const wrapper = mountView()
    await flushPromises()

    const detailBtn = wrapper.findAll('button').find((b) => b.text() === '详情')
    await detailBtn!.trigger('click')
    await flushPromises()

    const drawer = wrapper.findComponent({ name: 'EndpointDetailDrawer' })
    expect(drawer.props('endpoint')).toEqual(endpoint)
    expect(drawer.props('subscriptionUrl')).toBe('http://localhost:3000/sub/abc123?token=tok')
    expect(wrapper.find('.endpoint-drawer-stub').exists()).toBe(true)
  })

  it('抽屉概况段启停/删除事件复用本页既有处理函数', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '详情')!
      .trigger('click')
    await flushPromises()

    await wrapper.find('.drawer-toggle-btn').trigger('click')
    await flushPromises()
    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints/7/toggle')

    await wrapper.find('.drawer-delete-btn').trigger('click')
    await flushPromises()
    expect(vi.mocked(client.delete)).toHaveBeenCalledWith('/endpoints/7')
    // 抽屉内删除后抽屉关闭
    expect(wrapper.find('.endpoint-drawer-stub').exists()).toBe(false)
  })
})
