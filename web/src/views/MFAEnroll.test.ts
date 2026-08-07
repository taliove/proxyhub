// Component tests for the forced MFA enrollment page (ticket 08):
// QR rendering from otpauth_url, the two-stage enroll calls, and the
// recovery-code acknowledgement gate that guards the finish button.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import MFAEnroll from './MFAEnroll.vue'
import { useAuthStore } from '@/stores/auth'
import { startMFAEnroll, confirmMFAEnroll } from '@/api/mfa'
import { generateQRCode } from '@/composables/useQRCode'

vi.mock('@/api/mfa', () => ({
  startMFAEnroll: vi.fn(),
  confirmMFAEnroll: vi.fn()
}))

vi.mock('@/composables/useQRCode', () => ({
  generateQRCode: vi.fn()
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }
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

const ElFormStub = defineComponent({
  name: 'ElForm',
  setup(_, { slots, expose }) {
    expose({ validate: async () => true })
    return () => h('form', { class: 'el-form-stub' }, slots.default?.())
  }
})

// el-button 桩:保留 disabled 语义,恢复码确认门就是靠它体现的
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
  mount(MFAEnroll, {
    global: {
      directives: { loading: {} },
      stubs: {
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-empty': SimpleSlotStub('ElEmpty'),
        'el-alert': ElAlertStub,
        'el-form': ElFormStub,
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-checkbox': ElCheckboxStub
      }
    }
  })

const RECOVERY_CODES = Array.from({ length: 10 }, (_, i) => `code-${i + 1}`)

// finishButton 取"完成"按钮:恢复码页的最后一颗按钮
const finishButton = (wrapper: ReturnType<typeof mountView>) => {
  const buttons = wrapper.findAll('.el-button-stub')
  return buttons[buttons.length - 1]
}

describe('MFAEnroll', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
    vi.mocked(startMFAEnroll).mockResolvedValue({
      secret: 'ABCDEFGHIJKLMNOP',
      otpauth_url: 'otpauth://totp/ProxyHub:alice?secret=ABCDEFGHIJKLMNOP&issuer=ProxyHub'
    })
    vi.mocked(generateQRCode).mockResolvedValue('data:image/png;base64,QR')
    vi.mocked(confirmMFAEnroll).mockResolvedValue({ ok: true, recovery_codes: RECOVERY_CODES })
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
  })

  it('挂载即领密钥，并把 otpauth_url 渲染成二维码', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(vi.mocked(startMFAEnroll)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(generateQRCode)).toHaveBeenCalledWith(
      'otpauth://totp/ProxyHub:alice?secret=ABCDEFGHIJKLMNOP&issuer=ProxyHub'
    )

    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('data:image/png;base64,QR')
    // 手动录入路径:密钥以 4 字符分组展示
    expect(wrapper.text()).toContain('ABCD EFGH IJKL MNOP')
  })

  it('二维码生成失败时保留密钥文本，不炸页面', async () => {
    vi.mocked(generateQRCode).mockRejectedValue(new Error('canvas unavailable'))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('ABCD EFGH IJKL MNOP')
  })

  it('6 位码确认后带 totp_code 调接口并展示 10 个恢复码', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('.el-input-stub').setValue('123456')
    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    expect(vi.mocked(confirmMFAEnroll)).toHaveBeenCalledWith('123456')
    const codes = wrapper.findAll('.mfa-enroll__code')
    expect(codes).toHaveLength(10)
    expect(codes[0].text()).toBe('code-1')
  })

  it('未勾选"我已保存"时完成按钮禁用，不放行', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', false, true)

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('.el-input-stub').setValue('123456')
    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    const finish = finishButton(wrapper)
    expect(finish.text()).toContain('完成')
    expect(finish.attributes('disabled')).toBeDefined()

    await finish.trigger('click')
    await flushPromises()

    expect(routerPush).not.toHaveBeenCalled()
    expect(useAuthStore().mustEnrollMFA).toBe(true)
  })

  it('勾选后完成：清除强制位并跳首页', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', false, true)
    // restore 会打 /me;这里让它失败,验证放行不依赖它成功
    vi.spyOn(authStore, 'restore').mockRejectedValue(new Error('offline'))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('.el-input-stub').setValue('123456')
    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    await wrapper.find('.el-checkbox-stub input').setValue(true)
    await flushPromises()

    const finish = finishButton(wrapper)
    expect(finish.attributes('disabled')).toBeUndefined()
    await finish.trigger('click')
    await flushPromises()

    expect(authStore.mustEnrollMFA).toBe(false)
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('复制全部恢复码写入剪贴板（换行分隔）', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('.el-input-stub').setValue('123456')
    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    const copyAll = wrapper
      .findAll('.el-button-stub')
      .find((b) => b.text().includes('复制全部恢复码'))
    await copyAll!.trigger('click')
    await flushPromises()

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(RECOVERY_CODES.join('\n'))
  })

  it('验证码错误时留在扫码页并清空输入', async () => {
    vi.mocked(confirmMFAEnroll).mockRejectedValue({
      response: { status: 400, data: { error: 'invalid verification code' } }
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('.el-input-stub').setValue('123456')
    await wrapper.find('form.el-form-stub').trigger('submit')
    await flushPromises()

    expect(wrapper.findAll('.mfa-enroll__code')).toHaveLength(0)
    expect((wrapper.find('.el-input-stub').element as HTMLInputElement).value).toBe('')
  })

  it('已绑定（409）时直接放行，不把用户锁在绑定页', async () => {
    const authStore = useAuthStore()
    authStore.setAuth('alice', 'user', false, true)
    vi.spyOn(authStore, 'restore').mockResolvedValue(true)
    vi.mocked(startMFAEnroll).mockRejectedValue({
      response: { status: 409, data: { error: 'mfa already enrolled' } }
    })

    mountView()
    await flushPromises()

    expect(authStore.mustEnrollMFA).toBe(false)
    expect(routerPush).toHaveBeenCalledWith('/')
  })
})
