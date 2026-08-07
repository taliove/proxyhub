import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import EndpointCreateDialog from './EndpointCreateDialog.vue'
import client from '@/api/client'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

vi.mock('@/composables/useTemplateList', () => ({
  useTemplateList: () => ({ templates: { value: [] }, loadTemplates: vi.fn() })
}))

// 精选对话框桩:提供「确认精选」按钮上抛 confirm(新建暂存链路,issue #80)
const NodePicksDialogStub = defineComponent({
  name: 'EndpointNodePicksDialog',
  props: {
    modelValue: { type: Boolean, default: false },
    endpoint: { type: Object, default: null },
    stagedPicks: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue', 'confirm'],
  setup(props, { emit }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'picks-dialog-stub' }, [
            h(
              'button',
              {
                class: 'picks-confirm-btn',
                onClick: () => emit('confirm', [{ key: 'hk1.example.com:443' }])
              },
              '确认精选'
            )
          ])
        : null
  }
})

const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'el-dialog-stub' }, [slots.default?.(), slots.footer?.()])
        : null
  }
})
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-input-inner',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
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

const mountDialog = () =>
  mount(EndpointCreateDialog, {
    props: { modelValue: true },
    global: {
      stubs: {
        'el-dialog': ElDialogStub,
        'el-form': SimpleSlotStub('ElForm'),
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-select': SimpleSlotStub('ElSelect'),
        'el-option': SimpleSlotStub('ElOption'),
        EndpointNodePicksDialog: NodePicksDialogStub
      }
    }
  })

describe('EndpointCreateDialog(新建订阅地址;公开名称 issue #38;暂存精选 issue #80/#85)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(client.post).mockResolvedValue({ id: 9 } as never)
  })

  it('新建表单携带可选公开名称(issue #38)', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 表单内输入框顺序:别名、公开名称(配置模板是 select)
    const inputs = wrapper.findAll('input.el-input-inner')
    expect(inputs.length).toBe(2)
    await inputs[0].setValue('老爸的手机')
    await inputs[1].setValue('家里宽带')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '创建')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints', {
      alias: '老爸的手机',
      public_name: '家里宽带'
    })
    expect(wrapper.emitted('created')).toBeTruthy()
  })

  it('新建表单公开名称留空则不下发该字段', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input.el-input-inner')[0].setValue('老爸的手机')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '创建')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints', { alias: '老爸的手机' })
  })

  it('选择节点暂存后,创建成功补 PUT node-picks(新格式对象数组)', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 精选入口初始为「全量(不精选)」;点击打开暂存模式选择器
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '全量(不精选)')!
      .trigger('click')
    await flushPromises()
    await wrapper.find('.picks-confirm-btn').trigger('click')
    await flushPromises()

    // 暂存回显在入口按钮上
    expect(wrapper.findAll('button').map((b) => b.text())).toContain('精选 1 个节点')

    await wrapper.findAll('input.el-input-inner')[0].setValue('新端点')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '创建')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints', { alias: '新端点' })
    expect(vi.mocked(client.put)).toHaveBeenCalledWith('/endpoints/9/node-picks', {
      node_picks: [{ key: 'hk1.example.com:443' }]
    })
  })

  it('未触碰精选:创建后不发 node-picks PUT(零回归)', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input.el-input-inner')[0].setValue('新端点')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '创建')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.put)).not.toHaveBeenCalledWith(
      '/endpoints/9/node-picks',
      expect.anything()
    )
  })
})
