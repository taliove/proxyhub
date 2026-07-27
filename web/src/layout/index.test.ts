// Component tests for the layout shell (ticket 09): the impersonation
// banner is rendered only when the auth store carries an acting user, and
// the exit button clears the session's acting_user_id via the API.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import Navbar from './components/Navbar.vue'
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

// Router stub: exit-switch navigates home then reloads.
const routerPush = vi.fn(async () => undefined)
const routerGo = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush, go: routerGo })
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

const mountNavbar = () =>
  mount(Navbar, {
    props: { collapsed: false },
    global: {
      stubs: {
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-tooltip': SimpleSlotStub('ElTooltip'),
        'el-dropdown': SimpleSlotStub('ElDropdown'),
        'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
        'el-dropdown-item': SimpleSlotStub('ElDropdownItem'),
        'el-tag': SimpleSlotStub('ElTag'),
        'el-button': defineComponent({
          name: 'ElButton',
          emits: ['click'],
          setup(_, { slots, emit }) {
            return () => h('button', { onClick: () => emit('click') }, slots.default?.())
          }
        })
      }
    }
  })

describe('layout/Navbar impersonation banner (ticket 09)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('hides the acting banner when no impersonation is active', () => {
    const wrapper = mountNavbar()
    expect(wrapper.find('[data-testid="acting-banner"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('正在查看')
  })

  it('renders the acting banner with the impersonated username', () => {
    const auth = useAuthStore()
    auth.setActingUser({ id: 7, username: 'alice' })

    const wrapper = mountNavbar()
    expect(wrapper.find('[data-testid="acting-banner"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('正在查看')
    expect(wrapper.text()).toContain('alice')
  })

  it('exit button calls /admin/exit-switch and clears acting state', async () => {
    vi.mocked(client.post).mockResolvedValue({ ok: true } as never)
    const auth = useAuthStore()
    auth.setActingUser({ id: 7, username: 'alice' })

    const wrapper = mountNavbar()
    await flushPromises()

    const exitBtn = wrapper.findAll('button').find((b) => b.text().includes('退出'))
    expect(exitBtn).toBeDefined()
    await exitBtn!.trigger('click')
    await flushPromises()

    expect(client.post).toHaveBeenCalledWith('/admin/exit-switch')
    expect(auth.actingUser).toBeNull()
    expect(routerPush).toHaveBeenCalledWith('/')
  })
})
