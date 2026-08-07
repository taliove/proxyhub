// 恢复码重新生成区块:二次确认 -> 展示新码 -> 必须勾选"我已保存"才放行关闭。
// API 层整体打桩(线上契约由后端 handler 测试守),这里只验证 UI 接线与门禁。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import RecoveryCodeRegenerate from './RecoveryCodeRegenerate.vue'
import * as api from '@/api/mfa'
import { ElMessage } from 'element-plus'

vi.mock('@/api/mfa', () => ({ regenerateRecoveryCodes: vi.fn() }))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

vi.mock('@element-plus/icons-vue', () => ({ DocumentCopy: 'DocumentCopy' }))

const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: { type: { type: String, default: '' }, disabled: { type: Boolean, default: false } },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        {
          class: `el-button-stub btn-${props.type}`,
          disabled: props.disabled,
          onClick: () => !props.disabled && emit('click')
        },
        slots.default?.()
      )
  }
})
// 关闭态的 dialog 不渲染,避免隐藏文案污染断言。
const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false }, title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'el-dialog-stub', 'data-title': props.title }, [
            slots.default?.(),
            slots.footer?.()
          ])
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
        class: 'el-input-stub',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
  }
})
const ElCheckboxStub = defineComponent({
  name: 'ElCheckbox',
  props: { modelValue: { type: Boolean, default: false } },
  emits: ['update:modelValue'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        {
          class: 'el-checkbox-stub',
          'data-checked': String(props.modelValue),
          onClick: () => emit('update:modelValue', !props.modelValue)
        },
        slots.default?.()
      )
  }
})
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  props: { title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () => h('div', { class: 'el-alert-stub' }, [props.title, slots.default?.()])
  }
})

type VM = {
  confirmVisible: boolean
  codesVisible: boolean
  recoveryCodes: string[]
  savedAcknowledged: boolean
}

const mountBlock = () =>
  mount(RecoveryCodeRegenerate, {
    global: {
      stubs: {
        'el-button': ElButtonStub,
        'el-dialog': ElDialogStub,
        'el-input': ElInputStub,
        'el-checkbox': ElCheckboxStub,
        'el-alert': ElAlertStub
      }
    }
  })

// openAndSubmit 走完"点入口 -> 填码 -> 确认"三步。
const openAndSubmit = async (code: string) => {
  const wrapper = mountBlock()
  await wrapper.find('.btn-warning').trigger('click')
  await flushPromises()
  await wrapper.find('.el-input-stub').setValue(code)
  const confirmBtn = wrapper
    .findAll('.el-dialog-stub button')
    .find((b) => b.text().includes('确认生成'))!
  await confirmBtn.trigger('click')
  await flushPromises()
  return wrapper
}

const CODES = Array.from({ length: 10 }, (_, i) => `code-${i + 1}`)

describe('RecoveryCodeRegenerate', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => undefined) } })
  })

  it('入口先弹二次确认框，不直接调接口', async () => {
    const wrapper = mountBlock()
    await wrapper.find('.btn-warning').trigger('click')
    await flushPromises()

    expect((wrapper.vm as unknown as VM).confirmVisible).toBe(true)
    expect(api.regenerateRecoveryCodes).not.toHaveBeenCalled()
  })

  it('确认后带 code 调 regenerate 并展示 10 个新恢复码', async () => {
    vi.mocked(api.regenerateRecoveryCodes).mockResolvedValue({ ok: true, recovery_codes: CODES })

    const wrapper = await openAndSubmit('123456')

    expect(api.regenerateRecoveryCodes).toHaveBeenCalledWith('123456')
    const vm = wrapper.vm as unknown as VM
    expect(vm.confirmVisible).toBe(false)
    expect(vm.codesVisible).toBe(true)
    expect(vm.recoveryCodes).toHaveLength(10)
    expect(wrapper.text()).toContain('code-1')
    expect(wrapper.text()).toContain('code-10')
  })

  it('未勾选"我已保存"时关闭按钮不放行，勾选后才能关', async () => {
    vi.mocked(api.regenerateRecoveryCodes).mockResolvedValue({ ok: true, recovery_codes: CODES })
    const wrapper = await openAndSubmit('123456')

    const closeBtn = () =>
      wrapper.findAll('.el-dialog-stub button').find((b) => b.text().includes('关闭'))!
    expect(closeBtn().attributes('disabled')).toBeDefined()
    await closeBtn().trigger('click')
    expect((wrapper.vm as unknown as VM).codesVisible).toBe(true)

    await wrapper.find('.el-checkbox-stub').trigger('click')
    await flushPromises()
    await closeBtn().trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as VM
    expect(vm.codesVisible).toBe(false)
    // 关闭后明文不留在内存里
    expect(vm.recoveryCodes).toEqual([])
  })

  it('复制按钮把恢复码按行写入剪贴板', async () => {
    vi.mocked(api.regenerateRecoveryCodes).mockResolvedValue({ ok: true, recovery_codes: CODES })
    const wrapper = await openAndSubmit('123456')

    const copyBtn = wrapper
      .findAll('.el-dialog-stub button')
      .find((b) => b.text().includes('复制全部恢复码'))!
    await copyBtn.trigger('click')
    await flushPromises()

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(CODES.join('\n'))
    expect(ElMessage.success).toHaveBeenCalledWith('恢复码已复制')
  })

  it('确认码错误时停在确认框，不展示恢复码', async () => {
    vi.mocked(api.regenerateRecoveryCodes).mockRejectedValue({
      response: { status: 400, data: { error: 'invalid confirmation code' } }
    })
    const wrapper = await openAndSubmit('000000')

    const vm = wrapper.vm as unknown as VM
    expect(vm.confirmVisible).toBe(true)
    expect(vm.codesVisible).toBe(false)
    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('不正确或已过期'))
  })

  it('空码不发请求（回车提交绕过 disabled 的兜底）', async () => {
    const wrapper = mountBlock()
    await wrapper.find('.btn-warning').trigger('click')
    await flushPromises()
    await (wrapper.vm as unknown as { submitRegenerate: () => Promise<void> }).submitRegenerate()
    await flushPromises()

    expect(api.regenerateRecoveryCodes).not.toHaveBeenCalled()
    expect(ElMessage.warning).toHaveBeenCalledWith('请输入动态码或恢复码')
  })
})
