// StatCards 组件测试:统计项渲染、系统状态条与空态默认值
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import StatCards from './StatCards.vue'
import client from '@/api/client'

vi.mock('@/api/client')

const statsPayload = {
  totalNodes: 120,
  availableNodes: 80,
  endpoints: 3,
  airports: 5,
  lastUpdate: '2026-07-22 10:00:00',
  avgLatency: 123
}

const mountStatCards = () =>
  mount(StatCards, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' }
      }
    }
  })

describe('StatCards', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染四个统计项与系统状态条', async () => {
    vi.mocked(client.get).mockResolvedValue(statsPayload)
    const wrapper = mountStatCards()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/dashboard/stats')
    const text = wrapper.text()
    expect(text).toContain('总节点数')
    expect(text).toContain('120')
    expect(text).toContain('可用节点')
    expect(text).toContain('80')
    expect(text).toContain('订阅地址')
    expect(text).toContain('机场数量')
    expect(text).toContain('最近更新')
    expect(text).toContain('2026-07-22 10:00:00')
    expect(text).toContain('平均延迟')
    expect(text).toContain('123')
  })

  it('请求未完成时渲染空态默认值', () => {
    vi.mocked(client.get).mockReturnValue(new Promise(() => {}))
    const wrapper = mountStatCards()

    const values = wrapper.findAll('.stat-value').map((el) => el.text())
    expect(values).toEqual(['0', '0', '0', '0'])
    expect(wrapper.text()).toContain('-')
  })

  it('请求失败时静默降级为空态(全局拦截器已提示)', async () => {
    vi.mocked(client.get).mockRejectedValue(new Error('network'))
    const wrapper = mountStatCards()
    await flushPromises()

    const values = wrapper.findAll('.stat-value').map((el) => el.text())
    expect(values).toEqual(['0', '0', '0', '0'])
  })
})
