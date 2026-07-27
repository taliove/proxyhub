import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import ChangePassword from './ChangePassword.vue'
import { useAuthStore } from '@/stores/auth'
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
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

// router 桩:只关心 push 目标
const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
  useRoute: () => ({ query: {} })
}))

// 布局 store 桩:只用到 isDark / toggleDark
vi.mock('@/stores/layout', () => ({
  useLayoutStore: () => ({ isDark: false, toggleDark: vi.fn() })
}))

const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })

// el-alert 桩:title 以 prop 传入(与 ChangePassword 用法一致),default 插槽也渲染
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  props: { title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'ElAlert-stub' }, [
        h('span', { class: 'title' }, props.title),
        slots.default?.()
      ])
  }
})

// el-input 桩:受控渲染 value 并上抛 update,便于直接驱动表单
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

// el-form 桩:真实 el-form 的 validate 依赖 EP 内部状态机,这里直接放行
const ElFormStub = defineComponent({
  name: 'ElForm',
  setup(_, { slots, expose }) {
    expose({ validate: async () => true })
    return () => h('form', { class: 'el-form-stub' }, slots.default?.())
  }
})

const mountView = () =>
  mount(ChangePassword, {
    global: {
      stubs: {
        Wordmark: SimpleSlotStub('Wordmark'),
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-alert': ElAlertStub,
        'el-form': ElFormStub,
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-button': defineComponent({
          name: 'ElButton',
          emits: ['click'],
          setup(_, { slots, emit }) {
            return () =>
              h(
                'button',
                { class: 'el-button-stub', onClick: () => emit('click') },
                slots.default?.()
              )
          }
        })
      }
    }
  })

describe('ChangePassword', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
  })

  it('mustChangePassword=true 时展示「首次登录请修改密码」提示', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', true)

    const wrapper = mountView()
    await flushPromises()

    const alert = wrapper.find('.ElAlert-stub')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('首次登录请修改密码')
  })

  it('mustChangePassword=false(自助改密)不展示强制提示', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', false)

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('.ElAlert-stub').exists()).toBe(false)
  })

  it('提交成功后调用 /me/password,清除强制位并跳转首页', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', true)
    vi.mocked(client.post).mockResolvedValue({ ok: true })

    const wrapper = mountView()
    await flushPromises()

    const inputs = wrapper.findAll('.el-input-stub')
    expect(inputs).toHaveLength(3)
    await inputs[0].setValue('old-pass-1')
    await inputs[1].setValue('newpass123')
    await inputs[2].setValue('newpass123')

    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/me/password', {
      old_password: 'old-pass-1',
      new_password: 'newpass123'
    })
    expect(authStore.mustChangePassword).toBe(false)
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('接口失败时不跳首页、不清除强制位', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', true)
    vi.mocked(client.post).mockRejectedValue(new Error('bad old password'))

    const wrapper = mountView()
    await flushPromises()

    const inputs = wrapper.findAll('.el-input-stub')
    await inputs[0].setValue('wrong-old')
    await inputs[1].setValue('newpass123')
    await inputs[2].setValue('newpass123')

    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    expect(routerPush).not.toHaveBeenCalled()
    expect(authStore.mustChangePassword).toBe(true)
  })
})
