import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import type { Airport, Node } from '@/types'
import AirportDetailDrawer from './AirportDetailDrawer.vue'
import client from '@/api/client'
import { copyNodeLink } from '@/composables/useNodeShare'

// Mock API client:抽屉只允许读 /api/nodes 池快照,不得触发任何测试/检活请求
vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

// Mock 分享链接接缝:只关心抽屉调用了它,不真的打 share-uri 接口
vi.mock('@/composables/useNodeShare', () => ({
  canGenerateShareLink: vi.fn(() => true),
  copyNodeLink: vi.fn(async () => {})
}))

// Mock 体检对话框:记录 open 调用,不拉起 SSE
const examOpen = vi.fn()
vi.mock('@/components/NodeExamDialog.vue', () => ({
  default: defineComponent({
    name: 'NodeExamDialog',
    setup(_, { expose }) {
      expose({ open: examOpen })
      return () => h('div', { class: 'exam-dialog-stub' })
    }
  })
}))

// Mock 报告段:抽屉测试只验证数据接线与事件上抛,报告呈现归 AirportTestReport.test.ts
vi.mock('@/components/AirportTestReport.vue', () => ({
  default: defineComponent({
    name: 'AirportTestReport',
    props: {
      runs: { type: Array, default: () => [] },
      loading: { type: Boolean, default: false }
    },
    emits: ['run-test'],
    setup(props, { emit }) {
      return () =>
        h('div', { class: 'test-report-stub', 'data-run-count': String(props.runs.length) }, [
          h('button', { class: 'retest-btn', onClick: () => emit('run-test', false) }, '重新测试'),
          h('button', { class: 'full-btn', onClick: () => emit('run-test', true) }, '测全部')
        ])
    }
  })
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn() }
}))

// el-table/el-table-column 测试桩:列按行渲染 scoped slot,并渲染列头 label
const ElTableStub = defineComponent({
  name: 'ElTable',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide('rows', toRef(props, 'data'))
    return () =>
      h(
        'div',
        { class: 'el-table-stub' },
        props.data.length > 0 ? slots.default?.() : slots.empty?.()
      )
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
const ElDrawerStub = defineComponent({
  name: 'ElDrawer',
  props: { modelValue: { type: Boolean, default: false }, title: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      props.modelValue
        ? h('div', { class: 'el-drawer-stub' }, [
            h('div', { class: 'drawer-title' }, props.title),
            slots.default?.()
          ])
        : null
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
const ElTagStub = defineComponent({
  name: 'ElTag',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
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

const poolNode: Node = {
  name: 'XX 香港 01',
  display_name: '🇭🇰 香港 JS-01',
  type: 'vmess',
  server: 'hk01.example.com',
  port: 443,
  tls: true,
  region: 'HK',
  source: '极速机场',
  latency: 120,
  available: true,
  node_key: 'hk01.example.com:443',
  blocked: false,
  stale: false,
  availability_source: 'real',
  detection_last_check: '2026-07-22T08:00:00Z'
}

const mountDrawer = (modelValue: boolean, nodeList: Node[] = [poolNode]) => {
  vi.mocked(client.get).mockImplementation(async (url: unknown) => {
    if (url === '/nodes') return { nodes: nodeList } as never
    if (url === '/airports/7/test/runs') return [] as never
    return {} as never
  })
  return mount(AirportDetailDrawer, {
    props: { modelValue, airport },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-drawer': ElDrawerStub,
        'el-descriptions': ElDescriptionsStub,
        'el-descriptions-item': ElDescriptionsItemStub,
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub
      }
    }
  })
}

describe('AirportDetailDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('打开抽屉只拉取池快照与测试记录(纯读取),不触发任何测试/检活写请求', async () => {
    mountDrawer(true)
    await flushPromises()

    const getCalls = vi.mocked(client.get).mock.calls.map(([url]) => String(url))
    expect(getCalls).toHaveLength(2)
    expect(getCalls).toContain('/nodes')
    expect(getCalls).toContain('/airports/7/test/runs')
    // 无任何 POST/PUT/DELETE(测试/检活/体检都是写请求;查看报告不产生新 run)
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
    expect(vi.mocked(client.put)).not.toHaveBeenCalled()
    expect(vi.mocked(client.delete)).not.toHaveBeenCalled()
  })

  it('关闭状态不拉取任何数据', async () => {
    mountDrawer(false)
    await flushPromises()
    expect(vi.mocked(client.get)).not.toHaveBeenCalled()
  })

  it('概况段展示机场信息,订阅 URL 可复制', async () => {
    const writeText = vi.fn(async () => {})
    Object.assign(navigator, { clipboard: { writeText } })

    const wrapper = mountDrawer(true)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('极速机场')
    expect(text).toContain('JS')
    expect(text).toContain('https://example.com/sub/token123')

    const copyBtn = wrapper.findAll('button').find((b) => b.text() === '复制')
    expect(copyBtn).toBeDefined()
    await copyBtn!.trigger('click')
    expect(writeText).toHaveBeenCalledWith('https://example.com/sub/token123')
  })

  it('概况段轻管理动作全部上抛(编辑/启停/删除/刷新/测试/二维码)', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const cases: Array<[string, string]> = [
      ['编辑', 'edit'],
      ['禁用', 'toggle'],
      ['删除', 'delete'],
      ['刷新', 'refresh'],
      ['测试', 'test'],
      ['二维码', 'qrcode']
    ]
    for (const [label, event] of cases) {
      const btn = wrapper.findAll('button').find((b) => b.text() === label)
      expect(btn, `button ${label}`).toBeDefined()
      await btn!.trigger('click')
      expect(wrapper.emitted(event)).toBeTruthy()
      expect(wrapper.emitted(event)![0]).toEqual([airport])
    }
  })

  it('池内节点明细:展示名称/地区/可用性/延迟/最近实测,行轻动作仅复制链接与体检,无屏蔽', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('池内节点明细')
    expect(text).toContain('🇭🇰 香港 JS-01')
    expect(text).toContain('HK')
    expect(text).toContain('可用')
    expect(text).toContain('120ms')
    // 最近实测为相对时间(检测时间为固定过去时刻,必落在 N天前/MM-DD 档)
    expect(text).not.toContain('—')

    const nodeButtons = wrapper.findAll('button').map((b) => b.text())
    expect(nodeButtons).toContain('复制链接')
    expect(nodeButtons).toContain('体检')
    expect(nodeButtons).not.toContain('屏蔽')
    expect(nodeButtons).not.toContain('取消屏蔽')
  })

  it('节点行「复制链接」调用 useNodeShare 接缝', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const btn = wrapper.findAll('button').find((b) => b.text() === '复制链接')
    await btn!.trigger('click')
    expect(copyNodeLink).toHaveBeenCalledWith(poolNode)
  })

  it('节点行「体检」按 node_key 打开 NodeExamDialog', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const btn = wrapper.findAll('button').find((b) => b.text() === '体检')
    await btn!.trigger('click')
    expect(examOpen).toHaveBeenCalledWith(
      { node_key: poolNode.node_key },
      poolNode.display_name,
      poolNode.server
    )
  })

  it('池内无节点时展示引导空态', async () => {
    const wrapper = mountDrawer(true, [])
    await flushPromises()
    expect(wrapper.text()).toContain('该机场当前在池内无节点')
  })

  it('最近测试段:拉取 runs 传给报告组件;重跑意图上抛 {airport, full}', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    // runs 已传给报告段(本用例 mock 为空数组)
    const report = wrapper.findComponent({ name: 'AirportTestReport' })
    expect(report.exists()).toBe(true)
    expect(report.props('runs')).toEqual([])

    await wrapper.find('.retest-btn').trigger('click')
    await wrapper.find('.full-btn').trigger('click')
    expect(wrapper.emitted('run-test')).toEqual([
      [{ airport, full: false }],
      [{ airport, full: true }]
    ])
    // 重跑只是上抛意图,抽屉自身不发 POST
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
  })

  it('reloadReport 暴露给父级:重跑完成后重新拉取测试记录', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()
    expect(
      vi.mocked(client.get).mock.calls.filter(([u]) => u === '/airports/7/test/runs')
    ).toHaveLength(1)

    await (wrapper.vm as unknown as { reloadReport: () => Promise<void> }).reloadReport()
    await flushPromises()
    expect(
      vi.mocked(client.get).mock.calls.filter(([u]) => u === '/airports/7/test/runs')
    ).toHaveLength(2)
  })
})
