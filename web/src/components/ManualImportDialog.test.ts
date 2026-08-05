import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { Airport } from '@/types'
import ManualImportDialog from './ManualImportDialog.vue'
import client from '@/api/client'
import { ElMessage } from 'element-plus'

vi.mock('@/api/client', () => ({
  default: { post: vi.fn() }
}))
vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), warning: vi.fn(), error: vi.fn() }
}))

// element-plus 未全局注册,用具名桩替身(与兄弟组件测试同一模式)
const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'ElDialog-stub' }, [slots.default?.(), slots.footer?.()])
        : null
  }
})
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('textarea', {
        class: 'ElInput-stub',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value)
      })
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: {
    type: { type: String, default: '' },
    loading: { type: Boolean, default: false },
    disabled: { type: Boolean, default: false }
  },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        {
          class: `ElButton-stub type-${props.type || 'default'}`,
          disabled: props.disabled,
          onClick: () => emit('click')
        },
        slots.default?.()
      )
  }
})
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  props: {
    title: { type: String, default: '' },
    description: { type: String, default: undefined }
  },
  setup(props) {
    return () => h('div', { class: 'ElAlert-stub' }, `${props.title}${props.description ?? ''}`)
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })

const manualAirport: Airport = {
  id: 1,
  name: '手动机场',
  url: '',
  abbr: '',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z',
  source_type: 'manual'
}
const urlAirport: Airport = { ...manualAirport, id: 2, name: '拉取机场', source_type: 'url' }

const mountDialog = (airport: Airport) =>
  mount(ManualImportDialog, {
    props: { modelValue: true, airport },
    global: {
      stubs: {
        'el-dialog': ElDialogStub,
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-alert': ElAlertStub,
        'el-table': SimpleSlotStub('ElTable'),
        'el-table-column': SimpleSlotStub('ElTableColumn'),
        'el-form': SimpleSlotStub('ElForm'),
        AirportUsageFields: SimpleSlotStub('AirportUsageFields')
      }
    }
  })

const pasteAndImport = async (wrapper: ReturnType<typeof mountDialog>, content: string) => {
  await wrapper.findComponent(ElInputStub).setValue(content)
  const importBtn = wrapper.findAllComponents(ElButtonStub).find((b) => b.text().includes('导入'))
  expect(importBtn).toBeTruthy()
  await importBtn!.trigger('click')
  await flushPromises()
}

describe('ManualImportDialog 确认流程(用户实测拍板)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('全部成功:ElMessage 成功提示 + 自动关闭 + imported 事件刷新列表', async () => {
    vi.mocked(client.post).mockResolvedValue({ imported: 2, failures: [] })
    const wrapper = mountDialog(manualAirport)

    await pasteAndImport(wrapper, 'ss://x@node1.example.com:8388#HK 01')

    expect(ElMessage.success).toHaveBeenCalledWith('成功导入 2 条')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
    expect(wrapper.emitted('imported')).toHaveLength(1)
    // 对话框内不留结果区(已关闭语义)
    expect(wrapper.find('.ElAlert-stub').exists()).toBe(false)
  })

  it('有失败行:对话框停留展示明细,导入禁用,完成变主按钮', async () => {
    vi.mocked(client.post).mockResolvedValue({
      imported: 1,
      failures: [{ line: 2, reason: 'unsupported protocol' }]
    })
    const wrapper = mountDialog(manualAirport)

    await pasteAndImport(wrapper, 'good\nbroken')

    // 停留:不发关闭,不用 ElMessage 成功提示
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(wrapper.emitted('imported')).toHaveLength(1)
    // 成功数 + 失败明细可见
    expect(wrapper.find('.ElAlert-stub').text()).toContain('成功导入 1 条')
    expect(wrapper.find('.ElAlert-stub').text()).toContain('1 行解析失败')
    expect(wrapper.find('.ElTable-stub').exists()).toBe(true)
    // 导入禁用,完成变主按钮
    const importBtn = wrapper.findAllComponents(ElButtonStub).find((b) => b.text().includes('导入'))
    const doneBtn = wrapper.findAllComponents(ElButtonStub).find((b) => b.text().includes('完成'))
    expect(importBtn?.props('disabled')).toBe(true)
    expect(doneBtn?.props('type')).toBe('primary')
  })

  it('修正内容后结果清除、导入恢复可用(Check LOW:不必关窗重贴)', async () => {
    vi.mocked(client.post).mockResolvedValue({
      imported: 1,
      failures: [{ line: 2, reason: 'unsupported protocol' }]
    })
    const wrapper = mountDialog(manualAirport)

    await pasteAndImport(wrapper, 'good\nbroken')
    expect(wrapper.find('.ElAlert-stub').exists()).toBe(true)

    // 编辑粘贴内容:结果区清除,导入按钮恢复
    await wrapper
      .findComponent(ElInputStub)
      .setValue('good\nss://fixed@node3.example.com:8388#SG 01')
    expect(wrapper.find('.ElAlert-stub').exists()).toBe(false)
    const importBtn = wrapper.findAllComponents(ElButtonStub).find((b) => b.text().includes('导入'))
    expect(importBtn?.props('disabled')).toBe(false)

    // 重试成功:正常走全成功流程
    vi.mocked(client.post).mockResolvedValue({ imported: 2, failures: [] })
    await importBtn!.trigger('click')
    await flushPromises()
    expect(ElMessage.success).toHaveBeenCalledWith('成功导入 2 条')
  })

  it('拉取型机场:提示一次性导入语义,隐藏用量字段,不随贴用量', async () => {
    vi.mocked(client.post).mockResolvedValue({ imported: 1, failures: [] })
    const wrapper = mountDialog(urlAirport)

    expect(wrapper.text()).toContain('一次性导入')
    expect(wrapper.text()).toContain('以 URL 拉取内容为准')
    expect(wrapper.find('.AirportUsageFields-stub').exists()).toBe(false)

    await pasteAndImport(wrapper, 'ss://x@node1.example.com:8388#HK 01')
    const body = vi.mocked(client.post).mock.calls[0][1] as Record<string, unknown>
    expect(body.usage_total).toBeUndefined()
    expect(body.usage_remaining).toBeUndefined()
  })

  it('手动机场:用量字段可见且随贴', async () => {
    vi.mocked(client.post).mockResolvedValue({ imported: 1, failures: [] })
    const wrapper = mountDialog(manualAirport)

    expect(wrapper.find('.AirportUsageFields-stub').exists()).toBe(true)

    await pasteAndImport(wrapper, 'ss://x@node1.example.com:8388#HK 01')
    const body = vi.mocked(client.post).mock.calls[0][1] as Record<string, unknown>
    expect(body.usage_total).toBe(0) // 全空发零值(显式清空可达)
  })

  it('409 冲突:警告提示且不关闭', async () => {
    vi.mocked(client.post).mockRejectedValue({ response: { status: 409 } })
    const wrapper = mountDialog(manualAirport)

    await pasteAndImport(wrapper, 'ss://x@node1.example.com:8388#HK 01')

    expect(ElMessage.warning).toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('imported')).toBeUndefined()
  })
})

describe('ManualImportDialog 文件导入', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // 触发文件选择:input.files 在 jsdom 只读,defineProperty 注入后派发 change
  const pickFile = async (wrapper: ReturnType<typeof mountDialog>, file: File) => {
    const input = wrapper.find('input.file-input')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    // FileReader 回调是独立任务,flushPromises 覆盖不到
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
  }

  it('读入本地订阅文件:内容填入粘贴框并展示文件名', async () => {
    const wrapper = mountDialog(manualAirport)
    await pickFile(
      wrapper,
      new File(['ss://x@node1.example.com:8388#HK 01'], 'sub.txt', { type: 'text/plain' })
    )

    expect(wrapper.findComponent(ElInputStub).props('modelValue')).toBe(
      'ss://x@node1.example.com:8388#HK 01'
    )
    expect(wrapper.text()).toContain('sub.txt')
  })

  it('超限文件(>1MiB):警告拦截,不读入、不发请求', async () => {
    const wrapper = mountDialog(manualAirport)
    await pickFile(wrapper, new File(['x'.repeat((1 << 20) + 1)], 'big.txt'))

    expect(ElMessage.warning).toHaveBeenCalledWith('文件过大(上限 1MiB),请拆分后分批导入')
    expect(wrapper.findComponent(ElInputStub).props('modelValue')).toBe('')
    expect(client.post).not.toHaveBeenCalled()
  })
})
