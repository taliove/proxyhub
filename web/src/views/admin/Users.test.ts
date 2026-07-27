// Component tests for the admin user-management page (ticket 05):
// list rendering, create/edit dialogs, disable/enable/delete/reset flows.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { ElMessage } from 'element-plus'
import type { AdminUser } from '@/api/users'
import Users from './Users.vue'
import client from '@/api/client'
import { useAuthStore } from '@/stores/auth'

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

// Router stub: enter-space pushes '/' then reloads; both are mocked so the
// test only asserts the switchUser API call and store update.
const routerPush = vi.fn(async () => undefined)
const routerGo = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, go: routerGo })
}))

// Table stubs render every row through the column scoped slots.
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
  props: {
    label: { type: String, default: '' },
    prop: { type: String, default: '' }
  },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    return () =>
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) =>
          h('div', { class: 'tc-row', key: i }, [
            // Render the prop value when the column has no scoped slot
            slots.default
              ? slots.default({ row })
              : String((row as Record<string, unknown>)[props.prop] ?? '')
          ])
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
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, [slots.default?.(), slots.footer?.()])
    }
  })
// Dialog stubs render nothing while closed so hidden dialog text never
// pollutes list assertions.
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

// The seeded super admin has no quota row server-side: quota is null and the
// table must render placeholders instead of crashing.
const superAdmin: AdminUser = {
  id: 1,
  username: 'admin',
  role: 'super_admin',
  disabled: false,
  must_change_password: false,
  created_at: '2026-01-01T00:00:00Z',
  quota: null,
  airport_count: 0,
  endpoint_count: 0
}

const normalUser: AdminUser = {
  id: 2,
  username: 'alice',
  role: 'user',
  disabled: false,
  must_change_password: false,
  created_at: '2026-01-02T00:00:00Z',
  last_login_at: '2026-07-20T08:00:00Z',
  quota: {
    max_airports: 5,
    max_endpoints: 10,
    xray_port_start: 20000,
    xray_port_end: 20010
  },
  airport_count: 2,
  endpoint_count: 4
}

const mountView = (list: AdminUser[] = [superAdmin, normalUser]) => {
  // The backend returns a bare array (no envelope).
  vi.mocked(client.get).mockResolvedValue(list as never)
  return mount(Users, {
    global: {
      directives: { loading: {} },
      stubs: {
        PageHeader: SimpleSlotStub('PageHeader'),
        'el-card': SimpleSlotStub('ElCard'),
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub,
        'el-dialog': ClosedDialogStub('ElDialog'),
        'el-form': SimpleSlotStub('ElForm'),
        'el-form-item': SimpleSlotStub('ElFormItem'),
        'el-input': SimpleSlotStub('ElInput'),
        'el-input-number': SimpleSlotStub('ElInputNumber')
      }
    }
  })
}

describe('admin/Users', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('loads the user list on mount and renders username/role/quota/status', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/admin/users')
    const text = wrapper.text()
    expect(text).toContain('alice')
    expect(text).toContain('超管')
    expect(text).toContain('普通用户')
    // usage rendered as count/max from top-level counts + quota caps
    expect(text).toContain('2/5')
    expect(text).toContain('4/10')
    // the quota-less super admin row renders placeholders, not a crash
    expect(text).toContain('0/-')
    expect(text).toContain('启用')
  })

  it('does not offer disable/delete actions on the super_admin row', async () => {
    const wrapper = mountView()
    await flushPromises()

    const rows = wrapper.findAll('.tc-row')
    const adminRowText = rows
      .filter((r) => r.text().includes('admin'))
      .map((r) => r.text())
      .join(' ')
    expect(adminRowText).not.toContain('禁用')
    expect(adminRowText).not.toContain('删除')
    expect(adminRowText).not.toContain('重置密码')
  })

  it('opens the create dialog with a pre-filled random password', async () => {
    const wrapper = mountView()
    await flushPromises()

    const createBtn = wrapper.findAll('button').find((b) => b.text() === '新建用户')
    await createBtn!.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    expect(dialog.props('modelValue')).toBe(true)
    // The generated password is exposed for copy via the component state.
    expect(
      (wrapper.vm as unknown as { createForm: { password: string } }).createForm.password
    ).toMatch(/^[A-Za-z0-9]{16}$/)
  })

  it('submits create with username/password and flat quota fields', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    const wrapper = mountView()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '新建用户')!
      .trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      createForm: { username: string; password: string }
      submitCreate: () => Promise<void>
    }
    vm.createForm.username = 'bob'
    await vm.submitCreate()
    await flushPromises()

    const [url, payload] = vi.mocked(client.post).mock.calls[0]
    expect(url).toBe('/admin/users')
    expect((payload as { username: string }).username).toBe('bob')
    expect((payload as { password: string }).password).toMatch(/^[A-Za-z0-9]{16}$/)
    // quota fields go out flat (backend adminCreateUserRequest), not nested
    expect((payload as { max_airports: number }).max_airports).toBeGreaterThanOrEqual(0)
    expect(payload).not.toHaveProperty('quota')
  })

  it('shows a localized hint when the username is reserved', async () => {
    vi.mocked(client.post).mockRejectedValue({
      response: { status: 400, data: { error: 'username is reserved' } },
      config: {}
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '新建用户')!
      .trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      createForm: { username: string }
      submitCreate: () => Promise<void>
    }
    vm.createForm.username = 'Guest'
    await vm.submitCreate()
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('系统保留名'))
    // dialog stays open so the name can be fixed in place
    expect(wrapper.findComponent({ name: 'ElDialog' }).props('modelValue')).toBe(true)
  })

  it('shows a taken-username message on 409', async () => {
    vi.mocked(client.post).mockRejectedValue({
      response: { status: 409, data: { error: 'username already taken' } },
      config: {}
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '新建用户')!
      .trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      createForm: { username: string }
      submitCreate: () => Promise<void>
    }
    vm.createForm.username = 'alice'
    await vm.submitCreate()
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('已被占用'))
  })

  it('disables a user after confirmation and reloads the list', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    const wrapper = mountView()
    await flushPromises()

    const disableBtn = wrapper.findAll('button').find((b) => b.text() === '禁用')
    expect(disableBtn).toBeDefined()
    await disableBtn!.trigger('click')
    await flushPromises()

    expect(client.post).toHaveBeenCalledWith('/admin/users/2/disable')
    // list reloaded after the action
    expect(vi.mocked(client.get).mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('enables a disabled user without confirmation', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    const disabledUser = { ...normalUser, disabled: true }
    const wrapper = mountView([superAdmin, disabledUser])
    await flushPromises()

    const enableBtn = wrapper.findAll('button').find((b) => b.text() === '启用')
    expect(enableBtn).toBeDefined()
    await enableBtn!.trigger('click')
    await flushPromises()

    expect(client.post).toHaveBeenCalledWith('/admin/users/2/enable')
  })

  it('deletes a user after confirmation', async () => {
    vi.mocked(client.delete).mockResolvedValue({} as never)
    const wrapper = mountView()
    await flushPromises()

    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === '删除')
    await deleteBtn!.trigger('click')
    await flushPromises()

    expect(client.delete).toHaveBeenCalledWith('/admin/users/2')
  })

  it('resets password after confirmation and shows the new password dialog', async () => {
    vi.mocked(client.post).mockResolvedValue({
      ok: true,
      password: 'newpass123',
      user_id: 2,
      username: 'alice'
    } as never)
    const wrapper = mountView()
    await flushPromises()

    const resetBtn = wrapper.findAll('button').find((b) => b.text() === '重置密码')
    await resetBtn!.trigger('click')
    await flushPromises()

    expect(client.post).toHaveBeenCalledWith('/admin/users/2/reset-password')
    const vm = wrapper.vm as unknown as {
      passwordResultVisible: boolean
      passwordResult: string
    }
    expect(vm.passwordResultVisible).toBe(true)
    expect(vm.passwordResult).toBe('newpass123')
  })

  it('opens the edit dialog with the row quota and submits an update', async () => {
    vi.mocked(client.put).mockResolvedValue({} as never)
    const wrapper = mountView()
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === '编辑')
    await editBtn!.trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      editForm: { id: number; max_airports: number; max_endpoints: number }
      submitEdit: () => Promise<void>
    }
    expect(vm.editForm.id).toBe(2)
    expect(vm.editForm.max_airports).toBe(5)
    vm.editForm.max_airports = 8
    await vm.submitEdit()
    await flushPromises()

    const [url, payload] = vi.mocked(client.put).mock.calls[0]
    expect(url).toBe('/admin/users/2')
    expect((payload as { max_airports: number }).max_airports).toBe(8)
    expect(payload).not.toHaveProperty('quota')
  })

  it('enter-space calls switchUser API, updates auth store, and navigates home', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    const wrapper = mountView()
    await flushPromises()

    const enterBtn = wrapper.findAll('button').find((b) => b.text() === '进入空间')
    expect(enterBtn).toBeDefined()
    await enterBtn!.trigger('click')
    await flushPromises()

    // POST goes to the switch-user endpoint with the target id.
    expect(client.post).toHaveBeenCalledWith('/admin/switch-user', { user_id: normalUser.id })
    // The auth store mirrors the impersonation target so the navbar banner renders.
    const auth = useAuthStore()
    expect(auth.actingUser).toEqual({ id: normalUser.id, username: normalUser.username })
    // Router push home triggers the post-switch reload.
    expect(routerPush).toHaveBeenCalledWith('/')
  })

  it('hides the enter-space action on disabled users', async () => {
    const disabled = { ...normalUser, disabled: true }
    const wrapper = mountView([superAdmin, disabled])
    await flushPromises()

    expect(wrapper.findAll('button').find((b) => b.text() === '进入空间')).toBeUndefined()
  })
})
