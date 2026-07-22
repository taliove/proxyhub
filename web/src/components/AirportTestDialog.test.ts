import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import AirportTestDialog from './AirportTestDialog.vue'
import type { Airport } from '@/types'
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
  ElMessage: { success: vi.fn(), error: vi.fn() }
}))

const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false }, title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'el-dialog-stub' }, [slots.default?.(), slots.footer?.()])
        : null
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })
// el-alert 桩:title 与 default 两个插槽都渲染
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  setup(_, { slots }) {
    return () => h('div', { class: 'ElAlert-stub' }, [slots.title?.(), slots.default?.()])
  }
})

const airport: Airport = {
  id: 7,
  name: '极速机场',
  url: 'https://example.com/sub/token123',
  abbr: 'JS',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z'
}

// POST /airports/{id}/test 返回 jobs 任务句柄(ADR 0027),不再是 run 行
const jobHandle = { jobId: 42, kind: 'airport_test', key: 'airport-7', started: true }

const completedRun = {
  id: 43,
  airport_id: 7,
  created_at: '2026-07-22T08:00:00Z',
  status: 'completed',
  is_full: false,
  overall_score: 90.5,
  dimensions_json: JSON.stringify({
    availability_score: 45,
    latency_score: 28,
    fetch_health_score: 10,
    region_score: 7.5,
    available_nodes: 9,
    total_nodes: 10,
    mean_latency_ms: 120,
    p95_latency_ms: 200,
    region_count: 3,
    region_distribution: { HK: 4, SG: 3, US: 3 },
    http_status: 200,
    parse_success_rate: 1,
    url_reachable: true
  })
}

// mockGet 按 URL 分发 jobs 详情/任务结果;terminal 为任务的终态
const mockGet = (terminal: string, run: unknown = completedRun) => {
  vi.mocked(client.get).mockImplementation(((url: string) => {
    if (url === '/jobs/42') {
      return Promise.resolve({
        id: 42,
        kind: 'airport_test',
        key: 'airport-7',
        status: terminal,
        created_at: '2026-07-22T08:00:00Z',
        updated_at: '2026-07-22T08:01:00Z'
      })
    }
    if (url === '/jobs/42/result') {
      return Promise.resolve({
        kind: 'airport_test',
        job_id: 42,
        reports: [],
        airport_test_run: run
      })
    }
    return Promise.reject(new Error(`unexpected GET ${url}`))
  }) as never)
}

const mountDialog = () =>
  mount(AirportTestDialog, {
    global: {
      directives: { loading: {} },
      stubs: {
        'el-dialog': ElDialogStub,
        'el-descriptions': SimpleSlotStub('ElDescriptions'),
        'el-descriptions-item': SimpleSlotStub('ElDescriptionsItem'),
        'el-button': SimpleSlotStub('ElButton'),
        'el-tag': SimpleSlotStub('ElTag'),
        'el-alert': ElAlertStub,
        'el-icon': SimpleSlotStub('ElIcon'),
        'el-progress': SimpleSlotStub('ElProgress'),
        'el-divider': SimpleSlotStub('ElDivider'),
        StatusDot: SimpleSlotStub('StatusDot'),
        AirportTestDiagnostic: SimpleSlotStub('AirportTestDiagnostic')
      }
    }
  })

// 推进一轮轮询(对话框以 1500ms 间隔轮询 /jobs/{id})
const advancePoll = async () => {
  await vi.advanceTimersByTimeAsync(1500)
  await flushPromises()
}

describe('AirportTestDialog(运行模式)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('仅挂载不产生任何请求(不再 watch 打开即跑)', async () => {
    mountDialog()
    await flushPromises()

    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
    expect(vi.mocked(client.get)).not.toHaveBeenCalled()
  })

  it('显式 start(抽样)发起 POST test,任务 done 后 emit finished 且只给结论', async () => {
    vi.mocked(client.post).mockResolvedValue(jobHandle as never)
    mockGet('done')
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { start: (a: Airport, full?: boolean) => void }).start(
      airport,
      false
    )
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/airports/7/test', { full: false })

    await advancePoll()

    expect(wrapper.emitted('finished')).toBeTruthy()
    // 完成态只给结论与去向,不展示完整报告(报告归抽屉)
    expect(wrapper.text()).toContain('测试完成')
    expect(wrapper.text()).toContain('90.5')
    expect(wrapper.text()).toContain('详情抽屉「最近测试」')
  })

  it('显式 start(测全部)以 full=true 发起', async () => {
    vi.mocked(client.post).mockResolvedValue(jobHandle as never)
    mockGet('done')
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { start: (a: Airport, full?: boolean) => void }).start(airport, true)
    await flushPromises()

    expect(vi.mocked(client.post)).toHaveBeenCalledWith('/airports/7/test', { full: true })
  })

  it('任务 failed 终态展示失败原因且不 emit finished', async () => {
    vi.mocked(client.post).mockResolvedValue(jobHandle as never)
    mockGet('failed', {
      ...completedRun,
      status: 'failed',
      overall_score: undefined,
      dimensions_json: '{}',
      error_message: 'subscription URL unreachable and no synced nodes in pool'
    })
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { start: (a: Airport, full?: boolean) => void }).start(
      airport,
      false
    )
    await flushPromises()
    await advancePoll()

    expect(wrapper.text()).toContain('测试失败')
    expect(wrapper.text()).toContain('subscription URL unreachable')
    expect(wrapper.emitted('finished')).toBeFalsy()
  })
})
