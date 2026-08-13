import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Endpoint } from '@/types'
import AdminUserEndpointsDialog from './AdminUserEndpointsDialog.vue'
import client from '@/api/client'
import type { AdminUser } from '@/api/users'

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

vi.mock('@/utils/useradmin', () => ({
  copyPassword: vi.fn()
}))

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
  props: { label: { type: String, default: '' }, prop: { type: String, default: '' } },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    return () =>
      h('div', [
        ...rows.value.map((row, i) =>
          h(
            'div',
            { key: i },
            slots.default?.({ row }) ?? String((row as Record<string, unknown>)[props.prop] ?? '')
          )
        )
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
const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () => (props.modelValue ? h('div', [slots.default?.(), slots.footer?.()]) : null)
  }
})

const user: AdminUser = {
  id: 42,
  username: 'alice',
  role: 'user',
  disabled: false,
  must_change_password: false,
  created_at: '2026-01-01T00:00:00Z',
  quota: null,
  airport_count: 0,
  endpoint_count: 1
}

const endpoint: Endpoint = {
  id: 7,
  alias: '手机订阅',
  path: 'abc123',
  token: 'tok',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z',
  name_mode: '',
  name_template: '',
  conditions: '',
  template_name: '',
  public_name: ''
}

const mountDialog = () =>
  mount(AdminUserEndpointsDialog, {
    props: { modelValue: true, user },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-dialog': ElDialogStub,
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-input': defineComponent({
          name: 'ElInput',
          props: { modelValue: { type: String, default: '' } },
          setup(props, { slots }) {
            return () =>
              h('div', [h('span', { class: 'input-value' }, props.modelValue), slots.append?.()])
          }
        })
      }
    }
  })

describe('AdminUserEndpointsDialog(issue #117)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('打开时按目标用户拉取订阅地址并逐行展示', async () => {
    vi.mocked(client.get).mockResolvedValue([endpoint] as never)
    const wrapper = mountDialog()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/admin/users/42/endpoints')
    expect(wrapper.text()).toContain('手机订阅')
    expect(wrapper.text()).toContain('abc123')
  })

  it('行内「重置链接」:确认后代重置,结果弹窗展示新链接', async () => {
    vi.mocked(client.get).mockResolvedValue([endpoint] as never)
    const rotated: Endpoint = {
      ...endpoint,
      path: 'new9',
      token: 'nt',
      grace_expires_at: '2026-08-16 00:00:00'
    }
    vi.mocked(client.post).mockResolvedValue(rotated as never)
    const wrapper = mountDialog()
    await flushPromises()

    const resetBtn = wrapper.findAll('button').find((b) => b.text() === '重置链接')
    await resetBtn!.trigger('click')
    await flushPromises()

    expect(client.post).toHaveBeenCalledWith('/admin/users/42/endpoints/7/reset-link')
    expect(wrapper.text()).toContain('http://localhost:3000/sub/new9?token=nt')
    expect(wrapper.text()).toContain('2026-08-16 00:00:00')
  })
})
