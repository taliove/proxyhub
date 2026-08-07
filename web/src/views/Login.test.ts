import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import Login from './Login.vue'
import { login, issueCaptcha, submitLoginMFA } from '@/api/auth'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  issueCaptcha: vi.fn(),
  submitLoginMFA: vi.fn()
}))

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

// AuthShell 只负责外壳(背景/主题/卡片/品牌头),测试关心的是它渲染出的插槽内容
vi.mock('@/components/AuthShell.vue', () => ({
  default: {
    name: 'AuthShell',
    props: ['subtitle'],
    template:
      '<div class="auth-shell-stub"><span class="subtitle">{{ subtitle }}</span><slot /></div>'
  }
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

// el-button 桩:保留 disabled 语义,MFA 提交门就是靠它体现的
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: { disabled: { type: Boolean, default: false } },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        {
          class: 'el-button-stub',
          disabled: props.disabled,
          onClick: () => {
            if (!props.disabled) emit('click')
          }
        },
        slots.default?.()
      )
  }
})

const ElCheckboxStub = defineComponent({
  name: 'ElCheckbox',
  props: { modelValue: { type: Boolean, default: false } },
  emits: ['update:modelValue'],
  setup(props, { emit, slots }) {
    return () =>
      h('label', { class: 'el-checkbox-stub' }, [
        h('input', {
          type: 'checkbox',
          checked: props.modelValue,
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).checked)
        }),
        slots.default?.()
      ])
  }
})

const mountView = () =>
  mount(Login, {
    global: {
      stubs: {
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-form': ElFormStub,
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-checkbox': ElCheckboxStub
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

  it('正常登录路径请求体不含验证码字段，零多余请求', async () => {
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

  it('答案为空时本地拦截，不打请求也不消耗失败计数', async () => {
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

  it('must_enroll_mfa 登录成功直接跳绑定页（ticket 08）', async () => {
    vi.mocked(login).mockResolvedValue({
      user: { role: 'user', must_change_password: false, must_enroll_mfa: true }
    })
    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(routerPush).toHaveBeenCalledWith('/mfa/enroll')
    expect(routerPush).not.toHaveBeenCalledWith('/')
  })

  it('改密优先于绑定：两个标志都在时先去改密页', async () => {
    vi.mocked(login).mockResolvedValue({
      user: { role: 'user', must_change_password: true, must_enroll_mfa: true }
    })
    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(routerPush).toHaveBeenCalledWith('/change-password')
    expect(routerPush).not.toHaveBeenCalledWith('/mfa/enroll')
  })
})

// ---------- 两步登录第二态(ticket 09) ----------

// mfaHandoff 是后端"密码通过但该 IP 未受信"的 200 响应形状
const mfaHandoff = (token = 'pending-token-1') => ({
  ok: false,
  mfa_required: true,
  mfa_pending_token: token
})

// mfa401 是第二步的统一失败响应(码错/过期/异 IP/超次都长这样)
const mfa401 = () => ({
  response: { status: 401, data: 'invalid verification code' }
})

// codeInput 取 MFA 态下唯一的输入框(第一态的凭据框此时已不在 DOM)
const codeInput = (wrapper: ReturnType<typeof mountView>) => wrapper.findAll('.el-input-stub')[0]

// mfaButtons: [验证, 切换码型, 返回重新登录]
const mfaButtons = (wrapper: ReturnType<typeof mountView>) => wrapper.findAll('.el-button-stub')

// enterMFA 走完第一步并进入 MFA 态
const enterMFA = async (wrapper: ReturnType<typeof mountView>, token = 'pending-token-1') => {
  vi.mocked(login).mockResolvedValueOnce(mfaHandoff(token))
  await fillCredentials(wrapper)
  await submit(wrapper)
}

describe('Login 两步验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
  })

  it('mfa_required 响应切换到验证码态，不再渲染凭据表单', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    // 凭据框消失、只剩一个码输入框:两态互斥,不互相遮挡
    expect(wrapper.findAll('.el-input-stub')).toHaveLength(1)
    expect(wrapper.find('.login-mfa').exists()).toBe(true)
    expect(wrapper.find('.login-mfa__trust').exists()).toBe(true)
    // MFA 态不需要图形验证码
    expect(wrapper.find('.captcha').exists()).toBe(false)
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('TOTP 验证成功进入系统，请求带 pending token 且默认不带 trust_ip', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    vi.mocked(submitLoginMFA).mockResolvedValueOnce({ ok: true, user: { role: 'user' } })
    await codeInput(wrapper).setValue('123 456')
    await submit(wrapper)

    expect(vi.mocked(submitLoginMFA)).toHaveBeenCalledWith({
      mfa_pending_token: 'pending-token-1',
      code: '123456'
    })
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('勾选信任此 IP 后请求带 trust_ip', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    await wrapper.find('.el-checkbox-stub input').setValue(true)
    vi.mocked(submitLoginMFA).mockResolvedValueOnce({ ok: true, user: { role: 'user' } })
    await codeInput(wrapper).setValue('654321')
    await submit(wrapper)

    expect(vi.mocked(submitLoginMFA)).toHaveBeenCalledWith({
      mfa_pending_token: 'pending-token-1',
      code: '654321',
      trust_ip: true
    })
  })

  it('切换到恢复码后按恢复码格式归一化并提交', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    await mfaButtons(wrapper)[1].trigger('click')
    // 切换后输入框清空,数字码在恢复码态下不再完整
    expect((codeInput(wrapper).element as HTMLInputElement).value).toBe('')

    vi.mocked(submitLoginMFA).mockResolvedValueOnce({ ok: true, user: { role: 'user' } })
    await codeInput(wrapper).setValue('abcd efgh jkmn')
    await submit(wrapper)

    expect(vi.mocked(submitLoginMFA)).toHaveBeenCalledWith({
      mfa_pending_token: 'pending-token-1',
      code: 'ABCD-EFGH-JKMN'
    })
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('码不完整时按钮禁用且不发请求', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    expect(mfaButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    await codeInput(wrapper).setValue('123')
    await submit(wrapper)
    expect(vi.mocked(submitLoginMFA)).not.toHaveBeenCalled()

    await codeInput(wrapper).setValue('123456')
    expect(mfaButtons(wrapper)[0].attributes('disabled')).toBeUndefined()
  })

  it('401 后停留在 MFA 态、清空输入、保留 pending token 可重试', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    vi.mocked(submitLoginMFA).mockRejectedValueOnce(mfa401())
    await codeInput(wrapper).setValue('000000')
    await submit(wrapper)

    expect(wrapper.find('.login-mfa').exists()).toBe(true)
    expect((codeInput(wrapper).element as HTMLInputElement).value).toBe('')
    expect(routerPush).not.toHaveBeenCalled()

    vi.mocked(submitLoginMFA).mockResolvedValueOnce({ ok: true, user: { role: 'user' } })
    await codeInput(wrapper).setValue('111111')
    await submit(wrapper)

    expect(vi.mocked(submitLoginMFA)).toHaveBeenLastCalledWith({
      mfa_pending_token: 'pending-token-1',
      code: '111111'
    })
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('返回重新登录退回第一态并清空密码', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    await mfaButtons(wrapper)[2].trigger('click')
    await flushPromises()

    // 回到第一态:用户名/密码两个框回来,密码已清空
    const inputs = wrapper.findAll('.el-input-stub')
    expect(inputs).toHaveLength(2)
    expect((inputs[0].element as HTMLInputElement).value).toBe('alice')
    expect((inputs[1].element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('.login-mfa').exists()).toBe(false)
  })

  it('403(挑战期间账号被禁用)自动退回第一态', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    vi.mocked(submitLoginMFA).mockRejectedValueOnce({
      response: { status: 403, data: 'account disabled' }
    })
    await codeInput(wrapper).setValue('123456')
    await submit(wrapper)

    expect(wrapper.find('.login-mfa').exists()).toBe(false)
    expect(wrapper.findAll('.el-input-stub')).toHaveLength(2)
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('MFA 成功但欠改密/欠绑定时走与第一态一致的分流', async () => {
    const wrapper = mountView()
    await enterMFA(wrapper)

    vi.mocked(submitLoginMFA).mockResolvedValueOnce({
      ok: true,
      user: { role: 'user', must_change_password: true }
    })
    await codeInput(wrapper).setValue('123456')
    await submit(wrapper)

    expect(routerPush).toHaveBeenCalledWith('/change-password')
    expect(routerPush).not.toHaveBeenCalledWith('/')
  })

  it('第一态验证码要求与第二态共存：MFA 态不显示验证码块，退回后仍在', async () => {
    vi.mocked(login).mockRejectedValueOnce(captcha401('invalid credentials'))
    vi.mocked(issueCaptcha).mockResolvedValue({ challenge_id: 'c1', image_base64: 'data:x' })

    const wrapper = mountView()
    await fillCredentials(wrapper)
    await submit(wrapper)
    expect(wrapper.find('.captcha').exists()).toBe(true)

    // 带验证码重试,这次密码过了但要 MFA
    vi.mocked(login).mockResolvedValueOnce(mfaHandoff())
    await wrapper.findAll('.el-input-stub')[2].setValue('8888')
    await submit(wrapper)

    expect(wrapper.find('.captcha').exists()).toBe(false)
    expect(wrapper.find('.login-mfa').exists()).toBe(true)

    // 退回第一态:验证码块回来了(后端仍要求)
    await mfaButtons(wrapper)[2].trigger('click')
    await flushPromises()
    expect(wrapper.find('.captcha').exists()).toBe(true)
  })
})
