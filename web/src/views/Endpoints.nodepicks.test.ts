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

// 精选对话框桩:记录 props;提供「确认精选」按钮上抛 confirm(新建暂存链路用)
const NodePicksDialogStub = defineComponent({
  name: 'EndpointNodePicksDialog',
  props: {
    modelValue: { type: Boolean, default: false },
    endpoint: { type: Object, default: null },
    stagedPicks: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue', 'saved', 'confirm'],
  setup(props, { emit }) {
    return () =>
      props.modelValue
        ? h(
            'div',
            {
              class: 'picks-dialog-stub',
              'data-endpoint-id': (props.endpoint as Endpoint | null)?.id ?? 'staged'
            },
            [
              h(
                'button',
                {
                  class: 'picks-confirm-btn',
                  onClick: () => emit('confirm', [{ key: 'hk1.example.com:443' }])
                },
                '确认精选'
              )
            ]
          )
        : null
  }
})

// el-table/el-table-column 测试桩:列按行渲染 scoped slot(照 Endpoints.test.ts 形制)
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
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { value: { type: String, default: '' }, modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { slots, emit }) {
    return () =>
      h('div', { class: 'el-input-stub' }, [
        h('input', {
          class: 'el-input-inner',
          value: props.value || props.modelValue,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
        }),
        slots.append?.()
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
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () =>
      h('span', { class: 'el-tag-stub', onClick: () => emit('click') }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, [slots.default?.(), slots.footer?.()])
    }
  })
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

// fixture 全合成(example.com;issue #80)
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
  public_name: '',
  node_picks: '["hk1.example.com:443","us1.example.com:8443:cdn.example.com"]'
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
        EndpointCreateDialog: ClosedDialogStub('EndpointCreateDialog'),
        EndpointNodePicksDialog: NodePicksDialogStub,
        EndpointDetailDrawer: ClosedDialogStub('EndpointDetailDrawer'),
        QRCodeDialog: ClosedDialogStub('QRCodeDialog')
      }
    }
  })
}

describe('Endpoints 精选节点(issue #80)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('列表精选列:已配精选显示「精选 N 个节点」,未配显示「全量」', async () => {
    const noPicks: Endpoint = { ...endpoint, id: 8, node_picks: '' }
    const wrapper = mountView([endpoint, noPicks])
    await flushPromises()

    const tags = wrapper.findAll('.el-tag-stub').map((t) => t.text())
    expect(tags).toContain('精选 2 个节点')
    expect(tags).toContain('全量')
  })

  it('node_picks 解析失败按空集(与后端降级语义一致),显示「全量」', async () => {
    const bad: Endpoint = { ...endpoint, node_picks: 'not-json' }
    const wrapper = mountView([bad])
    await flushPromises()
    expect(wrapper.findAll('.el-tag-stub').map((t) => t.text())).toContain('全量')
  })

  it('点击精选标签打开选择器并传入该端点(编辑模式)', async () => {
    const wrapper = mountView()
    await flushPromises()

    const tag = wrapper.findAll('.el-tag-stub').find((t) => t.text() === '精选 2 个节点')!
    await tag.trigger('click')
    await flushPromises()

    const stub = wrapper.find('.picks-dialog-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-endpoint-id')).toBe('7')
  })
  it('「新建订阅地址」打开创建对话框(表单与暂存精选链路归 EndpointCreateDialog)', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('.EndpointCreateDialog-stub').exists()).toBe(false)

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '新建订阅地址')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('.EndpointCreateDialog-stub').exists()).toBe(true)
  })
})
