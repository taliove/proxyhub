// Sidebar 版本自报测试:版本接口返回后展示在侧栏底部;折叠态与接口失败时不渲染
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import Sidebar from './Sidebar.vue'
import client from '@/api/client'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/' }),
  useRouter: () => ({})
}))

vi.mock('../nav', () => ({
  getMenuSections: () => []
}))

const Stub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })

const mountSidebar = (collapsed: boolean) =>
  mount(Sidebar, {
    props: { collapsed },
    global: {
      stubs: {
        'el-menu': Stub('ElMenu'),
        'el-menu-item': Stub('ElMenuItem'),
        'el-divider': Stub('ElDivider'),
        'el-icon': Stub('ElIcon'),
        BrandMark: Stub('BrandMark'),
        Wordmark: Stub('Wordmark')
      }
    }
  })

describe('Sidebar 版本自报', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('展示 release 版本(加 v 前缀)', async () => {
    vi.mocked(client.get).mockResolvedValue({ initialized: true, version: '0.1.0-rc.1' })
    const wrapper = mountSidebar(false)
    await flushPromises()
    expect(wrapper.find('.ph-sidebar__version').text()).toBe('v0.1.0-rc.1')
  })

  it('dev 构建原样显示 dev', async () => {
    vi.mocked(client.get).mockResolvedValue({ initialized: true, version: 'dev' })
    const wrapper = mountSidebar(false)
    await flushPromises()
    expect(wrapper.find('.ph-sidebar__version').text()).toBe('dev')
  })

  it('折叠态不渲染版本', async () => {
    vi.mocked(client.get).mockResolvedValue({ initialized: true, version: '1.2.3' })
    const wrapper = mountSidebar(true)
    await flushPromises()
    expect(wrapper.find('.ph-sidebar__version').exists()).toBe(false)
  })

  it('状态接口失败时静默降级不渲染', async () => {
    vi.mocked(client.get).mockRejectedValue(new Error('network down'))
    const wrapper = mountSidebar(false)
    await flushPromises()
    expect(wrapper.find('.ph-sidebar__version').exists()).toBe(false)
  })
})
