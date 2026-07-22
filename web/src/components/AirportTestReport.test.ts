import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import AirportTestReport from './AirportTestReport.vue'
import type { TestRun } from '@/composables/useAirportTest'
import client from '@/api/client'

// 报告段是纯展示组件,不得发任何请求(查看 ≠ 重跑)
vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
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
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    return () =>
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) =>
          h('div', { class: 'tc-row', key: i }, slots.default?.({ row }))
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

const mountReport = (runs: TestRun[], loading = false) =>
  mount(AirportTestReport, {
    props: { runs, loading },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-descriptions': ElDescriptionsStub,
        'el-descriptions-item': ElDescriptionsItemStub,
        'el-tag': SimpleSlotStub('ElTag'),
        'el-alert': ElAlertStub,
        AirportTestTrend: SimpleSlotStub('AirportTestTrend'),
        StatusDot: SimpleSlotStub('StatusDot')
      }
    }
  })

const completedDims = {
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
  url_reachable: true,
  sampled_nodes: [
    { name: '🇭🇰 香港 TA-01', region: 'HK', available: true, latency_ms: 120 },
    { name: 'XX 新加坡 01', region: 'SG', available: false, latency_ms: 0, error: 'timeout' }
  ]
}

const completedRun: TestRun = {
  id: 11,
  airport_id: 7,
  created_at: '2026-07-22T08:00:00Z',
  status: 'completed',
  overall_score: 90.5,
  is_full: false,
  dimensions_json: JSON.stringify(completedDims)
}

describe('AirportTestReport', () => {
  it('查看报告不产生任何请求(打开不重跑)', async () => {
    mountReport([completedRun])
    await flushPromises()

    expect(vi.mocked(client.get)).not.toHaveBeenCalled()
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
  })

  it('空态:从未测过时引导显式测试', async () => {
    const wrapper = mountReport([])
    await flushPromises()

    expect(wrapper.text()).toContain('尚未测试过')
    // 重跑按钮存在且上抛意图
    const retest = wrapper.findAll('button').find((b) => b.text() === '重新测试')
    const full = wrapper.findAll('button').find((b) => b.text() === '测全部')
    expect(retest).toBeDefined()
    expect(full).toBeDefined()

    await retest!.trigger('click')
    await full!.trigger('click')
    expect(wrapper.emitted('run-test')).toEqual([[false], [true]])
  })

  it('失败态:最近一次 run failed 时展示错误,不展示评分报告', async () => {
    const failedRun: TestRun = {
      id: 12,
      airport_id: 7,
      created_at: '2026-07-22T09:00:00Z',
      status: 'failed',
      is_full: false,
      dimensions_json: '{}',
      error_message: 'subscription URL unreachable and no synced nodes in pool'
    }
    const wrapper = mountReport([failedRun])
    await flushPromises()

    expect(wrapper.text()).toContain('上次测试失败')
    expect(wrapper.text()).toContain('subscription URL unreachable')
    expect(wrapper.text()).not.toContain('综合得分')
  })

  it('completed run:事实汇总 + 维度构成拆解(标准权重 50/30/10/10)', async () => {
    const wrapper = mountReport([completedRun])
    await flushPromises()

    const text = wrapper.text()
    // 事实汇总
    expect(text).toContain('90.5')
    expect(text).toContain('9 / 10')
    expect(text).toContain('120 ms')
    expect(text).toContain('3 个地区')
    expect(text).toContain('HTTP 200')
    // 维度构成拆解:得分与权重
    expect(text).toContain('可用率(权重 50%)')
    expect(text).toContain('延迟表现(权重 30%)')
    expect(text).toContain('拉取健康(权重 10%)')
    expect(text).toContain('地区覆盖(权重 10%)')
    expect(text).toContain('45.0 分')
    expect(text).toContain('28.0 分')
    expect(text).toContain('7.5 分')
    // 抽样口径标识
    expect(text).toContain('抽样')
  })

  it('URL 不可达:拉取健康 N/A,权重按 5:3:1 重归一呈现', async () => {
    const unreachableRun: TestRun = {
      ...completedRun,
      dimensions_json: JSON.stringify({
        ...completedDims,
        url_reachable: false,
        fetch_health_score: 0,
        http_status: 0
      })
    }
    const wrapper = mountReport([unreachableRun])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('订阅 URL 不可达')
    expect(text).toContain('拉取健康(N/A,权重已重归一)')
    expect(text).toContain('可用率(权重 55.6%)')
    expect(text).toContain('延迟表现(权重 33.3%)')
    expect(text).toContain('地区覆盖(权重 11.1%)')
    expect(text).toContain('N/A(URL 不可达)')
  })

  it('抽样节点明细:逐节点展示名称/地区/可用性/延迟', async () => {
    const wrapper = mountReport([completedRun])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('抽样节点明细')
    expect(text).toContain('🇭🇰 香港 TA-01')
    expect(text).toContain('XX 新加坡 01')
    expect(text).toContain('可用')
    expect(text).toContain('不可用')
    expect(text).toContain('120ms')
  })

  it('旧 run 无抽样明细:降级为只显示汇总并说明', async () => {
    const oldRun: TestRun = {
      ...completedRun,
      dimensions_json: JSON.stringify({
        ...completedDims,
        sampled_nodes: undefined
      })
    }
    const wrapper = mountReport([oldRun])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('未保存抽样节点明细')
    // 汇总仍在
    expect(text).toContain('9 / 10')
  })
})
