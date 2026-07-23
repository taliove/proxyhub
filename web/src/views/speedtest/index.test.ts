import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Node } from '@/types'
import type { SpeedtestResult } from '@/api/speedtest'
import SpeedtestPage from './index.vue'
import client from '@/api/client'
import { runSpeedtest } from './runner'
import router from '@/router'
import { getMenuSections } from '@/layout/nav'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

// 测速流量原语不下真网络:mock runner,按 callbacks 走一遍阶段并返回固定产出
vi.mock('./runner', () => ({
  runSpeedtest: vi.fn()
}))

const routeQuery: Record<string, unknown> = {}
vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
    useRoute: () => ({ query: routeQuery, meta: { title: '本机实测' } })
  }
})

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

// el-select 桩:透出当前 modelValue 供预填断言
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: { modelValue: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-select-stub', 'data-model-value': props.modelValue }, slots.default?.())
  }
})
const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: { value: { type: String, default: '' }, label: { type: String, default: '' } },
  setup(props) {
    return () => h('div', { class: 'el-option-stub' }, props.label)
  }
})
// el-table/el-table-column 桩:列按行渲染 scoped slot(与 Endpoints.test.ts 同模式)
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
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })
// 抽屉桩:关闭态不渲染(原始记录只在打开时出现,避免污染聚合行断言)
const DrawerStub = defineComponent({
  name: 'ElDrawer',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () =>
      props.modelValue ? h('div', { class: 'el-drawer-stub' }, slots.default?.()) : null
  }
})

const globalStubs = {
  'el-select': ElSelectStub,
  'el-option': ElOptionStub,
  'el-table': ElTableStub,
  'el-table-column': ElTableColumnStub,
  'el-button': ElButtonStub,
  'el-tag': ElTagStub,
  'el-drawer': DrawerStub,
  'el-card': SimpleSlotStub('ElCard'),
  'el-alert': SimpleSlotStub('ElAlert')
}

const node = (over: Partial<Node> = {}): Node =>
  ({
    name: 'HK 节点',
    display_name: '香港 01',
    type: 'ss',
    server: 'hk.example.com',
    port: 443,
    tls: false,
    region: 'HK',
    source: '机场A',
    latency: 0,
    available: true,
    node_key: 'hk.example.com:443',
    blocked: false,
    stale: false,
    availability_source: 'real',
    ...over
  }) as Node

const record = (over: Partial<SpeedtestResult> = {}): SpeedtestResult => ({
  id: 1,
  node_key: '',
  down_mbps: 200,
  up_mbps: 100,
  idle_latency_ms: 20,
  jitter_ms: 2,
  client_info: 'agent',
  created_at: '2026-07-23T10:00:00Z',
  ...over
})

const mockGet = client.get as ReturnType<typeof vi.fn>
const mockPost = client.post as ReturnType<typeof vi.fn>
const mockRun = runSpeedtest as ReturnType<typeof vi.fn>

beforeEach(() => {
  vi.clearAllMocks()
  for (const k of Object.keys(routeQuery)) delete routeQuery[k]
  mockGet.mockImplementation((url: string) => {
    if (url === '/nodes') return Promise.resolve({ nodes: [node()], last_update: '' })
    if (url === '/speedtest/results')
      return Promise.resolve([
        record({ id: 3, node_key: 'gone.example.com:1', created_at: '2026-07-22T10:00:00Z' }),
        record({ id: 2, node_key: 'hk.example.com:443' }),
        record({ id: 1 })
      ])
    return Promise.reject(new Error(`unexpected GET ${url}`))
  })
  mockPost.mockResolvedValue({ id: 4 })
  mockRun.mockImplementation(
    async (
      _nodeKey: string,
      callbacks: {
        onPhase?: (p: string) => void
        onSample?: (p: string, m: number) => void
      }
    ) => {
      callbacks.onPhase?.('latency')
      callbacks.onPhase?.('download')
      callbacks.onPhase?.('upload')
      return { downMbps: 150, upMbps: 80, idleLatencyMs: 50, jitterMs: 6 }
    }
  )
})

const mountPage = () =>
  mount(SpeedtestPage, { global: { directives: { loading: {} }, stubs: globalStubs } })

describe('路由注册(主导航入口)', () => {
  it('本机实测出现在导航菜单', () => {
    const sections = getMenuSections(router)
    const items = sections.flatMap((s) => s.items)
    expect(items.some((i) => i.path === '/speedtest' && i.title === '本机实测')).toBe(true)
  })
})

describe('本机实测页', () => {
  it('挂载即拉节点池与历史;?node_key= 预填标注下拉', async () => {
    routeQuery.node_key = 'hk.example.com:443'
    const wrapper = mountPage()
    await flushPromises()

    expect(mockGet).toHaveBeenCalledWith('/nodes', expect.anything())
    expect(mockGet).toHaveBeenCalledWith('/speedtest/results')
    expect(wrapper.find('.el-select-stub').attributes('data-model-value')).toBe(
      'hk.example.com:443'
    )
  })

  it('历史按节点聚合:孤儿标注显示"已失效",节点行显示与直连基线的差值', async () => {
    const wrapper = mountPage()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('已失效') // gone.example.com:1 不在池
    expect(text).toContain('直连') // 直连基线行
    // 节点行 Δ下行 = 200 - 200 = 0? 该行 down 200 与直连相同;Δ延迟 = 20-20=0,显示 +0/0 皆可,此处只核列头存在
    expect(text).toContain('Δ下行')
    expect(text).toContain('Δ延迟')
  })

  it('一键实测:完成后自动落库(带当前标注)并刷新历史', async () => {
    routeQuery.node_key = 'hk.example.com:443'
    const wrapper = mountPage()
    await flushPromises()

    const startBtn = wrapper.findAll('button').find((b) => b.text().includes('开始实测'))
    expect(startBtn).toBeDefined()
    await startBtn!.trigger('click')
    await flushPromises()

    expect(mockRun).toHaveBeenCalledOnce()
    expect(mockPost).toHaveBeenCalledWith(
      '/speedtest/results',
      expect.objectContaining({ node_key: 'hk.example.com:443', down_mbps: 150, up_mbps: 80 })
    )
    // 初次加载 + 实测后刷新 = 两次历史拉取
    const historyCalls = mockGet.mock.calls.filter(([url]) => url === '/speedtest/results')
    expect(historyCalls.length).toBe(2)
  })

  it('预填节点已不在池:提示并不选中', async () => {
    routeQuery.node_key = 'gone.example.com:1'
    const wrapper = mountPage()
    await flushPromises()

    const { ElMessage } = await import('element-plus')
    expect(ElMessage.warning).toHaveBeenCalled()
    expect(wrapper.find('.el-select-stub').attributes('data-model-value')).toBe('')
  })
})
