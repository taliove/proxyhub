import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import Setup from './Setup.vue'
import { getStatus } from '@/api/status'
import client from '@/api/client'
import { ElMessage } from 'element-plus'

vi.mock('@/api/status', () => ({
  getStatus: vi.fn()
}))

vi.mock('@/api/client', () => ({
  default: { post: vi.fn() }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush })
}))

// AuthShell is a pure shell (background/card/brand); the tests care about
// the slotted wizard content only.
vi.mock('@/components/AuthShell.vue', () => ({
  default: {
    name: 'AuthShell',
    props: ['subtitle'],
    template: '<div class="auth-shell-stub"><slot /></div>'
  }
}))

const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })

// el-input stub: controlled render emitting update, so tests can fill fields
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-input-stub',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
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

const mountView = () =>
  mount(Setup, {
    global: {
      stubs: {
        'el-steps': SimpleSlotStub('ElSteps'),
        'el-step': SimpleSlotStub('ElStep'),
        'el-alert': SimpleSlotStub('ElAlert'),
        'el-form': SimpleSlotStub('ElForm'),
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-input-number': SimpleSlotStub('ElInputNumber'),
        'el-result': SimpleSlotStub('ElResult'),
        'el-button': ElButtonStub
      }
    }
  })

// reachSecurityStep fills valid credentials and advances to step 1
const reachSecurityStep = async (wrapper: ReturnType<typeof mountView>) => {
  const inputs = wrapper.findAll('.el-input-stub')
  await inputs[0].setValue('alice')
  await inputs[1].setValue('secret-pass')
  await inputs[2].setValue('secret-pass')
  await wrapper.find('.el-button-stub').trigger('click')
  await flushPromises()
}

describe('Setup 初始化守卫', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('系统已初始化时挂载即提示并跳转 /login', async () => {
    vi.mocked(getStatus).mockResolvedValue({ initialized: true })

    mountView()
    await flushPromises()

    expect(vi.mocked(ElMessage.warning)).toHaveBeenCalledWith('系统已初始化')
    expect(routerPush).toHaveBeenCalledWith('/login')
  })

  it('未初始化时停留在向导页，不跳转', async () => {
    vi.mocked(getStatus).mockResolvedValue({ initialized: false })

    const wrapper = mountView()
    await flushPromises()

    expect(routerPush).not.toHaveBeenCalled()
    expect(wrapper.findAll('.el-input-stub')).toHaveLength(3)
  })

  it('提交遇 400 显示「系统已初始化」而非「初始化失败」', async () => {
    vi.mocked(getStatus).mockResolvedValue({ initialized: false })
    vi.mocked(client.post).mockRejectedValue({ response: { status: 400 } })

    const wrapper = mountView()
    await flushPromises()
    await reachSecurityStep(wrapper)

    // step 1 renders [上一步, 下一步]; the second one submits
    const buttons = wrapper.findAll('.el-button-stub')
    await buttons[1].trigger('click')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(ElMessage.error)).toHaveBeenCalledWith('系统已初始化')
    expect(vi.mocked(ElMessage.error)).not.toHaveBeenCalledWith('初始化失败')
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('非 400 失败保留通用文案「初始化失败」', async () => {
    vi.mocked(getStatus).mockResolvedValue({ initialized: false })
    vi.mocked(client.post).mockRejectedValue({ response: { status: 500 } })

    const wrapper = mountView()
    await flushPromises()
    await reachSecurityStep(wrapper)

    const buttons = wrapper.findAll('.el-button-stub')
    await buttons[1].trigger('click')
    await flushPromises()

    expect(vi.mocked(ElMessage.error)).toHaveBeenCalledWith('初始化失败')
    expect(vi.mocked(ElMessage.error)).not.toHaveBeenCalledWith('系统已初始化')
  })
})
