// ActivityFeed 组件测试:running 优先排序、最近 5 条截断、空态与跳转
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ActivityFeed from './ActivityFeed.vue'
import { listJobs, type Job } from '@/api/jobs'

vi.mock('@/api/jobs')

const pushMock = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock })
}))

// 造任务:时间按 "2026-07-22 HH:MM:SS" 递增,便于字符串排序断言
const makeJob = (overrides: Partial<Job>): Job => ({
  id: 0,
  kind: 'batch_exam',
  key: 'k',
  status: 'done',
  created_at: '2026-07-22 09:00:00',
  updated_at: '2026-07-22 09:00:00',
  ...overrides
})

const mountFeed = () =>
  mount(ActivityFeed, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        ElTag: { template: '<span class="el-tag"><slot /></span>' }
      }
    }
  })

describe('ActivityFeed', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('running 任务全部在前,其后是非 running 最近 5 条(按 updated_at 倒序)', async () => {
    const finished = Array.from({ length: 7 }, (_, i) =>
      makeJob({
        id: i + 1,
        kind: 'batch_exam',
        status: 'done',
        updated_at: `2026-07-22 0${i + 1}:00:00`
      })
    )
    const running = [
      makeJob({ id: 100, kind: 'refresh', key: 'all', status: 'running' }),
      makeJob({ id: 101, kind: 'retag_all', status: 'running' })
    ]
    // 后端按 created_at 倒序返回;这里乱序给 finished,验证前端按 updated_at 重排
    vi.mocked(listJobs).mockResolvedValue([...finished, ...running])

    const wrapper = mountFeed()
    await flushPromises()

    const items = wrapper.findAll('.feed-item')
    expect(items).toHaveLength(7)
    // 前两条是 running
    expect(items[0].text()).toContain('刷新')
    expect(items[1].text()).toContain('晚间标签重算')
    // 后五条是 updated_at 最近的 5 条(07..03),最旧两条(01/02)被截断
    const times = wrapper.findAll('.item-time').map((el) => el.text())
    expect(times.slice(2)).toEqual([
      '2026-07-22 07:00:00',
      '2026-07-22 06:00:00',
      '2026-07-22 05:00:00',
      '2026-07-22 04:00:00',
      '2026-07-22 03:00:00'
    ])
  })

  it('条目渲染 kind 标签 / 状态 / 范围 / 触发源,点击跳任务中心', async () => {
    vi.mocked(listJobs).mockResolvedValue([
      makeJob({
        id: 1,
        kind: 'refresh',
        key: 'all',
        status: 'failed',
        params: JSON.stringify({ trigger: 'scheduled' })
      })
    ])
    const wrapper = mountFeed()
    await flushPromises()

    const item = wrapper.find('.feed-item')
    expect(item.text()).toContain('刷新')
    expect(item.text()).toContain('失败')
    expect(item.text()).toContain('全部机场')
    expect(item.text()).toContain('定时')

    await item.trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Jobs' })
  })

  it('无任务时显示空态', async () => {
    vi.mocked(listJobs).mockResolvedValue([])
    const wrapper = mountFeed()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toBe('暂无任务')
  })

  it('请求未完成时显示加载中', () => {
    vi.mocked(listJobs).mockReturnValue(new Promise(() => {}))
    const wrapper = mountFeed()

    expect(wrapper.find('.panel-empty').text()).toBe('加载中...')
  })

  it('请求失败时静默降级为空态(全局拦截器已提示)', async () => {
    vi.mocked(listJobs).mockRejectedValue(new Error('network'))
    const wrapper = mountFeed()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toBe('暂无任务')
  })
})
