import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import Login from './Login.vue'
import { login, issueCaptcha } from '@/api/auth'

vi.mock('@/api/auth', () => ({ login: vi.fn(), issueCaptcha: vi.fn() }))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

const routerPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
  useRoute: () => ({ query: {} })
}))

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

// el-input 桩:受控渲染并上抛 update,便于直接驱动表单
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

const ElButtonStub = defineComponent({
  name: 'ElButton',
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () =>
      h('button', { class: 'el-button-stub', onClick: () => emit('click') }, slots.default?.())
  }
})

const mountView = () =>
  mount(Login, {
    global: {
      stubs: {
        Wordmark: SimpleSlotStub('Wordmark'),
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-form': ElFormStub,
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-button': ElButtonStub
      }
    }
  })

// fillCredentials 填用户名/密码(前两个 input 恒为凭据框)
const fillCredentials = async (wrapper: ReturnType<typeof mountView>) => {
  const inputs = wrapper.findAll('.el-input-stub')
  await inputs[0].setValue('alice')
  await inputs[1].setValue('secret-pass')
}

const submit = async (wrapper: ReturnType<typeof mountView>) => {
  await wrapper.find('form.el-form-stub').trigger('submit')
  await flushPromises()
}

// captcha401 是后端 401 + captcha_required 的响应形状
const captcha401 = (error: string) => ({
  response: { status: 401, data: { error, captcha_required: true } }
})

describe('Login 验证码', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
  })

  it('首次打开无验证码块且不请求签发端点', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('.captcha').exists()).toBe(false)
    expect(wrapper.findAll('.el-input-stub')).toHaveLength(2)
    expect(vi.mocked(issueCaptcha)).not.toHaveBeenCalled()
  })

  it('正常登录路径请求体不含验证码字段,零多余请求', async () => {
    vi.mocked(login).mockResolvedValue({ user: { role: 'user', must_change_password: false } })
    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(vi.mocked(login)).toHaveBeenCalledWith({ username: 'alice', password: 'secret-pass' })
    expect(vi.mocked(issueCaptcha)).not.toHaveBeenCalled()
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('401 带 captcha_required 后渲染验证码块', async () => {
    vi.mocked(login).mockRejectedValue(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha).mockResolvedValue({
      challenge_id: 'c1',
      image_base64: 'data:image/png;base64,AAA'
    })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(vi.mocked(issueCaptcha)).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.captcha').exists()).toBe(true)
    expect(wrapper.find('.captcha__img').attributes('src')).toBe('data:image/png;base64,AAA')
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('激活后每次提交都带 captcha_id/captcha_answer', async () => {
    vi.mocked(login).mockRejectedValueOnce(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha).mockResolvedValue({ challenge_id: 'c1', image_base64: 'data:x' })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    vi.mocked(login).mockResolvedValueOnce({ user: { role: 'user' } })
    const captchaInput = wrapper.findAll('.el-input-stub')[2]
    await captchaInput.setValue('8888')
    await submit(wrapper)

    expect(vi.mocked(login)).toHaveBeenLastCalledWith({
      username: 'alice',
      password: 'secret-pass',
      captcha_id: 'c1',
      captcha_answer: '8888'
    })
  })

  it('验证码答错后自动换新 challenge 并清空输入', async () => {
    vi.mocked(login).mockRejectedValue(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha)
      .mockResolvedValueOnce({ challenge_id: 'c1', image_base64: 'data:one' })
      .mockResolvedValueOnce({ challenge_id: 'c2', image_base64: 'data:two' })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    await wrapper.findAll('.el-input-stub')[2].setValue('wrong')
    vi.mocked(login).mockRejectedValueOnce(captcha401('captcha required'))
    await submit(wrapper)

    expect(vi.mocked(issueCaptcha)).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.captcha__img').attributes('src')).toBe('data:two')
    expect((wrapper.findAll('.el-input-stub')[2].element as HTMLInputElement).value).toBe('')
  })

  it('答案为空时本地拦截,不打请求也不消耗失败计数', async () => {
    vi.mocked(login).mockRejectedValueOnce(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha).mockResolvedValue({ challenge_id: 'c1', image_base64: 'data:x' })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)
    expect(vi.mocked(login)).toHaveBeenCalledTimes(1)

    await submit(wrapper)
    expect(vi.mocked(login)).toHaveBeenCalledTimes(1)
  })

  it('换一张按钮请求新 challenge', async () => {
    vi.mocked(login).mockRejectedValueOnce(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha)
      .mockResolvedValueOnce({ challenge_id: 'c1', image_base64: 'data:one' })
      .mockResolvedValueOnce({ challenge_id: 'c2', image_base64: 'data:two' })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    await wrapper.find('.captcha__image').trigger('click')
    await flushPromises()

    expect(vi.mocked(issueCaptcha)).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.captcha__img').attributes('src')).toBe('data:two')
  })

  it('must_change_password 登录成功仍跳改密页', async () => {
    vi.mocked(login).mockResolvedValue({ user: { role: 'user', must_change_password: true } })
    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(routerPush).toHaveBeenCalledWith('/change-password')
  })
})
