import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { Endpoint } from '@/types'
import EndpointTestSection from './EndpointTestSection.vue'
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

const ElButtonStub = defineComponent({
  name: 'ElButton',
  emits: ['click'],
  setup(_, { slots, emit }) {
    return () => h('button', { onClick: () => emit('click') }, slots.default?.())
  }
})
const ElDescriptionsStub = defineComponent({
  name: 'ElDescriptions',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-descriptions-stub' }, slots.default?.())
  }
})
const ElDescriptionsItemStub = defineComponent({
  name: 'ElDescriptionsItem',
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'desc-item' }, [
        h('span', { class: 'desc-label' }, props.label),
        slots.default?.()
      ])
  }
})
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  }
})
// el-alert 桩:title 与 default 两个插槽都渲染
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-alert-stub' }, [slots.title?.(), slots.default?.()])
  }
})
// el-progress 桩:渲染 format() 产出的进度文案
const ElProgressStub = defineComponent({
  name: 'ElProgress',
  props: {
    percentage: { type: Number, default: 0 },
    format: { type: Function, default: undefined }
  },
  setup(props) {
    return () =>
      h(
        'div',
        { class: 'el-progress-stub' },
        props.format ? props.format() : `${props.percentage}%`
      )
  }
})

const endpoint: Endpoint = {
  id: 7,
  alias: '老爸的手机',
  path: 'abc123',
  token: 'tok',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z',
  name_mode: '',
  name_template: '',
  conditions: '',
  template_name: '',
  public_name: '',
  availability: { available: 3, total: 5 }
}

const testResultPayload = {
  pull: {
    clash: { valid: true, node_count: 5, duration_ms: 3 },
    v2ray: { valid: true, node_count: 5, duration_ms: 2 }
  },
  snapshot: { total: 5, available: 3, mean_latency_ms: 140, region_count: 2, regions: ['HK', 'US'] }
}

const runningProbe = {
  run_id: 'run-1',
  endpoint_id: 7,
  full: false,
  status: 'running',
  total: 5,
  sampled: 4,
  checked: 1
}

// getImpl:实测轮询的 GET 分支
const mountSection = (getImpl?: (u: string) => unknown) => {
  vi.mocked(client.get).mockImplementation(async (url: unknown) => {
    const u = String(url)
    if (getImpl) return getImpl(u) as never
    return {} as never
  })
  return mount(EndpointTestSection, {
    props: { endpoint },
    global: {
      stubs: {
        'el-button': ElButtonStub,
        'el-descriptions': ElDescriptionsStub,
        'el-descriptions-item': ElDescriptionsItemStub,
        'el-tag': ElTagStub,
        'el-alert': ElAlertStub,
        'el-progress': ElProgressStub
      }
    }
  })
}

const clickButton = async (wrapper: ReturnType<typeof mountSection>, label: string) => {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  expect(btn, `button ${label}`).toBeDefined()
  await btn!.trigger('click')
}

describe('EndpointTestSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('挂载不自动测：无任何请求，展示引导空态', async () => {
    const wrapper = mountSection()
    await flushPromises()

    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
    expect(vi.mocked(client.get)).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('尚未测试')
  })

  it('拉取验证：显式点击发起 POST /test 并展示双格式结果与池快照', async () => {
    vi.mocked(client.post).mockResolvedValue(testResultPayload as never)
    const wrapper = mountSection()
    await flushPromises()

    await clickButton(wrapper, '拉取验证')
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints/7/test')
    const text = wrapper.text()
    expect(text).toContain('合法')
    expect(text).toContain('5 节点')
    expect(text).toContain('3 / 5')
    expect(text).toContain('140 ms')
    expect(text).toContain('2 个地区')
    expect(text).toContain('HK/US')
  })

  it('现场实测：POST probe 后轮询进度，完成后刷新拉取验证与池快照', async () => {
    vi.useFakeTimers()
    vi.mocked(client.post).mockImplementation(async (url: unknown) => {
      const u = String(url)
      if (u === '/endpoints/7/test/probe') return { ...runningProbe } as never
      if (u === '/endpoints/7/test') return testResultPayload as never
      return {} as never
    })
    const wrapper = mountSection((u) => {
      if (u === '/endpoints/7/test/probe/run-1') {
        return { ...runningProbe, status: 'completed', checked: 4 }
      }
      return {}
    })
    await flushPromises()

    await clickButton(wrapper, '现场实测')
    await flushPromises()
    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints/7/test/probe', { full: false })
    // 进行中:展示进度 checked / sampled
    expect(wrapper.text()).toContain('现场实测进行中（抽样）')
    expect(wrapper.find('.el-progress-stub').text()).toBe('1 / 4')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(vi.mocked(client.get)).toHaveBeenCalledWith('/endpoints/7/test/probe/run-1')
    expect(wrapper.text()).toContain('实测完成（抽样，共检活 4 个节点）')
    // 完成后刷新快照(重跑拉取验证)
    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints/7/test')
  })

  it('测全部以 full=true 发起实测', async () => {
    vi.mocked(client.post).mockResolvedValue({ ...runningProbe, full: true } as never)
    const wrapper = mountSection()
    await flushPromises()

    await clickButton(wrapper, '测全部')
    await flushPromises()
    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/endpoints/7/test/probe', { full: true })
    expect(wrapper.text()).toContain('现场实测进行中（全量）')
  })

  it('实测轮询 404(run 重启丢失/过期):停止轮询并提示重跑', async () => {
    vi.useFakeTimers()
    vi.mocked(client.post).mockResolvedValue({ ...runningProbe } as never)
    const wrapper = mountSection((u) => {
      if (u === '/endpoints/7/test/probe/run-1') {
        const err = new Error('Request failed with status code 404') as Error & {
          response?: { status?: number }
        }
        err.response = { status: 404 }
        throw err
      }
      return {}
    })
    await flushPromises()
    await clickButton(wrapper, '现场实测')
    await flushPromises()

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(wrapper.text()).toContain('实测进度已失效')
    expect(wrapper.text()).toContain('请重新发起实测')

    // 轮询已停止:继续推进不再产生新的进度请求
    const pollCallsBefore = vi
      .mocked(client.get)
      .mock.calls.filter(([u]) => u === '/endpoints/7/test/probe/run-1').length
    await vi.advanceTimersByTimeAsync(4500)
    await flushPromises()
    const pollCallsAfter = vi
      .mocked(client.get)
      .mock.calls.filter(([u]) => u === '/endpoints/7/test/probe/run-1').length
    expect(pollCallsAfter).toBe(pollCallsBefore)
  })

  it('实测 run 返回 failed 时展示失败原因', async () => {
    vi.useFakeTimers()
    vi.mocked(client.post).mockResolvedValue({ ...runningProbe } as never)
    const wrapper = mountSection((u) => {
      if (u === '/endpoints/7/test/probe/run-1') {
        return { ...runningProbe, status: 'failed', error: 'probe core exploded' }
      }
      return {}
    })
    await flushPromises()
    await clickButton(wrapper, '现场实测')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(wrapper.text()).toContain('实测失败')
    expect(wrapper.text()).toContain('probe core exploded')
  })
})
