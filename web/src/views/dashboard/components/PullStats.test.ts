// PullStats 组件测试:三块数据视图(汇总/趋势/单地址明细)的加载与空态
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PullStats from './PullStats.vue'
import client from '@/api/client'

vi.mock('@/api/client')

// vue-echarts 真实组件依赖 ResizeObserver/jsdom 不具备的布局能力,整体替换为轻量桩
vi.mock('vue-echarts', () => ({
  default: { name: 'VChart', template: '<div class="v-chart" />' }
}))

const globalPayload = { total_pulls: 120, unique_ips: 34, active_endpoints: 2 }

const trendPayload = {
  trend: [
    { date: '2026-07-20', endpoint_id: 1, alias: '老爸的手机', count: 5 },
    { date: '2026-07-21', endpoint_id: 1, alias: '老爸的手机', count: 8 }
  ]
}

const endpointsPayload = [
  {
    id: 1,
    alias: '老爸的手机',
    path: 'abc123',
    token: 'tok-1',
    enabled: true,
    created_at: '2026-07-01 00:00:00',
    name_mode: '' as const,
    name_template: '',
    conditions: ''
  }
]

// 按 URL 分发 mock 响应
const mockGetByUrl = (overrides: Record<string, unknown> = {}) => {
  const table: Record<string, unknown> = {
    '/stats/global': globalPayload,
    '/stats/trend?days=7': trendPayload,
    '/endpoints': endpointsPayload,
    ...overrides
  }
  vi.mocked(client.get).mockImplementation((url: string) => {
    if (url in table) return Promise.resolve(table[url]) as never
    return Promise.reject(new Error(`unexpected url: ${url}`)) as never
  })
}

const mountPullStats = () =>
  mount(PullStats, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        ElRadioGroup: { template: '<div class="el-radio-group"><slot /></div>' },
        ElRadioButton: { template: '<span class="el-radio-button"><slot /></span>' },
        ElSelect: { template: '<div class="el-select"><slot /></div>' },
        ElOption: { template: '<span class="el-option" />' },
        ElEmpty: {
          props: ['description'],
          template: '<div class="el-empty">{{ description }}</div>'
        },
        IPStatsTable: { template: '<div class="ip-stats-table" />' }
      }
    }
  })

describe('PullStats', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('挂载后请求三块数据并渲染全局汇总', async () => {
    mockGetByUrl()
    const wrapper = mountPullStats()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/stats/global')
    expect(client.get).toHaveBeenCalledWith('/stats/trend?days=7')
    expect(client.get).toHaveBeenCalledWith('/endpoints')

    const text = wrapper.text()
    expect(text).toContain('总拉取次数')
    expect(text).toContain('120')
    expect(text).toContain('独立 IP 数')
    expect(text).toContain('34')
    expect(text).toContain('活跃订阅 (24h)')
    expect(text).toContain('2')
  })

  it('有趋势数据时渲染趋势图，并默认选中首个订阅地址展示 IP 明细', async () => {
    mockGetByUrl()
    const wrapper = mountPullStats()
    await flushPromises()

    expect(wrapper.find('.v-chart').exists()).toBe(true)
    expect(wrapper.find('.ip-stats-table').exists()).toBe(true)
  })

  it('无趋势数据时降级为空态文案', async () => {
    mockGetByUrl({ '/stats/trend?days=7': { trend: [] } })
    const wrapper = mountPullStats()
    await flushPromises()

    expect(wrapper.find('.v-chart').exists()).toBe(false)
    expect(wrapper.text()).toContain('暂无拉取数据')
  })

  it('无订阅地址时明细区提示选择', async () => {
    mockGetByUrl({ '/endpoints': [] })
    const wrapper = mountPullStats()
    await flushPromises()

    expect(wrapper.find('.ip-stats-table').exists()).toBe(false)
    expect(wrapper.text()).toContain('请选择一个订阅地址')
  })

  it('请求失败时保留空态默认值（全局拦截器已 toast）', async () => {
    vi.mocked(client.get).mockRejectedValue(new Error('network'))
    const wrapper = mountPullStats()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('总拉取次数')
    expect(text).toContain('暂无拉取数据')
    expect(text).toContain('请选择一个订阅地址')
  })
})
