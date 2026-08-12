// AlertPanel 组件测试:四类异常聚合渲染、阈值边界、24h 窗口、未测试弱提示、降级与跳转
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AlertPanel from './AlertPanel.vue'
import client from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import type { Job } from '@/api/jobs'
import type { Airport } from '@/types'

vi.mock('@/api/client')

const pushMock = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock })
}))

const AUDIT_EVENTS_URL =
  '/audit/events?event_type=login_failure,honeypot_ban,threshold_ban&time_range=24h&limit=500'

// 异常任务一次请求:status 逗号多值经 URLSearchParams 编码为 %2C
const JOBS_ALERT_URL = '/jobs?status=failed%2Cinterrupted'

// 造本地时间串 "YYYY-MM-DD HH:mm:ss"(与 jobs 表 updated_at 格式一致),msAgo 为距现在的毫秒数
const tsAgo = (msAgo: number) => {
  const d = new Date(Date.now() - msAgo)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

const makeAirport = (overrides: Partial<Airport>): Airport => ({
  id: 1,
  name: '机场A',
  url: 'https://example.com/sub',
  abbr: 'AA',
  enabled: true,
  created_at: '2026-01-01 00:00:00',
  ...overrides
})

const makeJob = (overrides: Partial<Job>): Job => ({
  id: 1,
  kind: 'refresh',
  key: 'all',
  status: 'failed',
  created_at: tsAgo(2 * 3600e3),
  updated_at: tsAgo(3600e3),
  ...overrides
})

// 默认四路全空;overrides 按 URL 覆盖单路响应,值设为 Error 表示该路失败
const mockAll = (overrides: Record<string, unknown> = {}) => {
  const table: Record<string, unknown> = {
    '/airports': [],
    [JOBS_ALERT_URL]: [],
    '/audit/banned': { banned: [] },
    [AUDIT_EVENTS_URL]: { events: [], total: 0 },
    ...overrides
  }
  vi.mocked(client.get).mockImplementation((url: string) => {
    if (!(url in table)) return Promise.reject(new Error(`unexpected url: ${url}`)) as never
    const v = table[url]
    return (v instanceof Error ? Promise.reject(v) : Promise.resolve(v)) as never
  })
}

const mountPanel = () =>
  mount(AlertPanel, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        ElTag: { template: '<span class="el-tag"><slot /></span>' }
      }
    }
  })

describe('AlertPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 异常面板的审计两路(封禁 IP/审计事件)只对超管发请求:
    // 本套件既有用例全部以超管身份挂载(普通用户用例见末尾)。
    setActivePinia(createPinia())
    useAuthStore().setAuth('admin', 'super_admin')
  })

  it('四类异常齐备时聚合渲染，各条目点击跳转对应页面', async () => {
    mockAll({
      '/airports': [makeAirport({ last_test_status: 'completed', last_test_score: 45.4 })],
      [JOBS_ALERT_URL]: [
        makeJob({ id: 1, status: 'failed' }),
        makeJob({
          id: 2,
          kind: 'batch_exam',
          key: 'k',
          status: 'interrupted',
          params: JSON.stringify({ scope: 'all' })
        })
      ],
      '/audit/banned': {
        banned: [
          {
            ip: '1.2.3.4',
            fail_count: 5,
            banned_until: new Date(Date.now() + 3600e3).toISOString()
          }
        ]
      },
      [AUDIT_EVENTS_URL]: {
        events: [
          { event_type: 'login_failure' },
          { event_type: 'login_failure' },
          { event_type: 'threshold_ban' }
        ],
        total: 3
      }
    })
    const wrapper = mountPanel()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('机场「机场A」最近测试得分 45 分')
    expect(text).toContain('刷新')
    expect(text).toContain('失败')
    expect(text).toContain('深度体检')
    expect(text).toContain('已中断')
    expect(text).toContain('IP 1.2.3.4 已被封禁')
    expect(text).toContain('24h 内登录失败 2 次')
    expect(text).toContain('24h 内阈值封禁 1 次')

    // 机场 1 + 任务 2 + 封禁 1 + 审计按类型聚合 2(登录失败/阈值封禁)
    const items = wrapper.findAll('.alert-item')
    expect(items).toHaveLength(6)

    await items[0].trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Airports' })
    // 任务类异常直达详情:?id= 定位(ticket 0023)
    await items[1].trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Jobs', query: { id: '1' } })
    await items[3].trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Audit' })
  })

  it('jobs 过滤参数：failed/interrupted 合并为一次逗号多值请求', async () => {
    mockAll()
    mountPanel()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith(JOBS_ALERT_URL)
    expect(client.get).not.toHaveBeenCalledWith('/jobs?status=failed')
    expect(client.get).not.toHaveBeenCalledWith('/jobs?status=interrupted')
  })

  it('四路全空时显示一切正常', async () => {
    mockAll()
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toBe('一切正常')
    expect(wrapper.find('.panel-hint').exists()).toBe(false)
  })

  it('低分阈值边界：60 分不算异常，低于 60 分算异常', async () => {
    mockAll({
      '/airports': [
        makeAirport({ id: 1, name: '刚好60', last_test_status: 'completed', last_test_score: 60 }),
        makeAirport({ id: 2, name: '差一分', last_test_status: 'completed', last_test_score: 59 })
      ]
    })
    const wrapper = mountPanel()
    await flushPromises()

    const items = wrapper.findAll('.alert-item')
    expect(items).toHaveLength(1)
    expect(items[0].text()).toContain('差一分')
    expect(wrapper.text()).not.toContain('刚好60')
  })

  it('从未测试的机场不算异常，单独弱提示且可点击；测试失败不算未测试', async () => {
    mockAll({
      '/airports': [
        makeAirport({ id: 1, name: '未测1' }),
        makeAirport({ id: 2, name: '未测2', last_test_status: null }),
        makeAirport({ id: 3, name: '测败', last_test_status: 'failed', last_test_score: null })
      ]
    })
    const wrapper = mountPanel()
    await flushPromises()

    // 无异常条目,面板仍是一切正常
    expect(wrapper.findAll('.alert-item')).toHaveLength(0)
    expect(wrapper.find('.panel-empty').text()).toBe('一切正常')

    const hint = wrapper.find('.panel-hint')
    expect(hint.text()).toBe('2 个机场尚未测试')
    await hint.trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Airports' })
  })

  it('超过 24h 的失败/中断任务不展示', async () => {
    mockAll({
      [JOBS_ALERT_URL]: [
        makeJob({ id: 1, updated_at: tsAgo(25 * 3600e3) }),
        makeJob({ id: 2, status: 'interrupted', updated_at: tsAgo(26 * 3600e3) })
      ]
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toBe('一切正常')
  })

  it('封禁已过期的 IP 不算当前封禁', async () => {
    mockAll({
      '/audit/banned': {
        banned: [
          {
            ip: '1.2.3.4',
            fail_count: 9,
            banned_until: new Date(Date.now() - 3600e3).toISOString()
          }
        ]
      }
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toBe('一切正常')
  })

  it('请求未完成时显示加载中', () => {
    vi.mocked(client.get).mockReturnValue(new Promise(() => {}) as never)
    const wrapper = mountPanel()

    expect(wrapper.find('.panel-empty').text()).toBe('加载中……')
  })

  it('单路接口失败时静默降级，其余异常照常展示', async () => {
    mockAll({
      '/airports': new Error('network'),
      '/audit/banned': {
        banned: [
          {
            ip: '5.6.7.8',
            fail_count: 3,
            banned_until: new Date(Date.now() + 3600e3).toISOString()
          }
        ]
      }
    })
    const wrapper = mountPanel()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('IP 5.6.7.8 已被封禁')
    expect(wrapper.findAll('.alert-item')).toHaveLength(1)
  })

  it('普通用户视角：不发审计两路请求（后端 403 专属）,其余区块照常', async () => {
    useAuthStore().clearAuth()
    useAuthStore().setAuth('member', 'user')
    mockAll({
      '/airports': [makeAirport({ last_test_status: 'completed', last_test_score: 45.4 })]
    })
    mountPanel()
    await flushPromises()

    const requested = vi.mocked(client.get).mock.calls.map((c) => c[0])
    expect(requested.some((u) => String(u).startsWith('/audit/'))).toBe(false)
    expect(requested).toContain('/airports')
  })
})
