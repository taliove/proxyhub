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
// el-input 测试桩:原生 input 透传 v-model
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue', 'clear'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-input-stub',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
  }
})
// el-dropdown 测试桩(与 AirportRowTestMenu.test 同一模式):菜单项内联渲染为 button,
// 点击经 provide/inject 派发 command,等价真实下拉的 command 事件。
const ElDropdownStub = defineComponent({
  name: 'ElDropdown',
  emits: ['command'],
  setup(_, { slots, emit }) {
    provide('dropdown-command', (cmd: unknown) => emit('command', cmd))
    return () => h('div', { class: 'el-dropdown-stub' }, [slots.default?.(), slots.dropdown?.()])
  }
})
const ElDropdownItemStub = defineComponent({
  name: 'ElDropdownItem',
  props: { command: { type: [String, Number, Boolean], default: undefined } },
  setup(props, { slots }) {
    const fire = inject<(cmd: unknown) => void>('dropdown-command')!
    return () =>
      h(
        'button',
        { class: 'el-dropdown-item-stub', onClick: () => fire(props.command) },
        slots.default?.()
      )
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('span', { class: `${name}-stub` }, slots.default?.())
    }
  })
// el-pagination 测试桩:渲染 total,暴露 next 按钮触发 current-change
const ElPaginationStub = defineComponent({
  name: 'ElPagination',
  props: {
    currentPage: { type: Number, default: 1 },
    pageSize: { type: Number, default: 10 },
    total: { type: Number, default: 0 }
  },
  emits: ['current-change'],
  setup(props, { emit }) {
    return () =>
      h('div', { class: 'el-pagination-stub' }, [
        h('span', { class: 'pg-total' }, String(props.total)),
        h(
          'button',
          { class: 'pg-next', onClick: () => emit('current-change', props.currentPage + 1) },
          'next'
        )
      ])
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

const mountDrawer = (modelValue: boolean, nodeList: Node[] = [poolNode], nodeTotal?: number) => {
  vi.mocked(client.get).mockImplementation(async (url: unknown) => {
    if (url === '/nodes')
      return {
        nodes: nodeList,
        total: nodeTotal ?? nodeList.length,
        page: 1,
        page_size: 10,
        total_pages: 1,
        last_update: ''
      } as never
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
        'el-tag': ElTagStub,
        'el-input': ElInputStub,
        'el-pagination': ElPaginationStub,
        'el-dropdown': ElDropdownStub,
        'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
        'el-dropdown-item': ElDropdownItemStub,
        'el-icon': SimpleSlotStub('ElIcon')
      }
    }
  })
}

// /nodes 请求调用史(url + config)
const nodesCalls = () =>
  vi.mocked(client.get).mock.calls.filter(([u]) => u === '/nodes') as Array<
    [unknown, { params: Record<string, unknown> }]
  >

describe('AirportDetailDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('打开抽屉只拉取池快照与测试记录（纯读取）,不触发任何测试/检活写请求', async () => {
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

  it('概况段展示机场信息，订阅 URL 可复制', async () => {
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

  it('概况段轻管理动作全部上抛（编辑/启停/删除/刷新/测试/二维码）', async () => {
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

  it('池内节点明细：展示名称/地区/可用性/延迟/最近实测，行轻动作仅复制链接与体检，无屏蔽', async () => {
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

  it('明细分段走服务端分页：默认 page=1&page_size=10,total 渲染自接口响应', async () => {
    const wrapper = mountDrawer(true, [poolNode], 42)
    await flushPromises()

    expect(nodesCalls()).toHaveLength(1)
    expect(nodesCalls()[0][1]).toEqual({
      params: { page: 1, page_size: 10, source: '极速机场' }
    })
    expect(wrapper.find('.pg-total').text()).toBe('42')
  })

  it('翻页触发对应页码的服务端请求', async () => {
    const wrapper = mountDrawer(true, [poolNode], 42)
    await flushPromises()

    await wrapper.find('.pg-next').trigger('click')
    await flushPromises()
    expect(nodesCalls()).toHaveLength(2)
    expect(nodesCalls()[1][1]).toEqual({
      params: { page: 2, page_size: 10, source: '极速机场' }
    })
  })

  it('搜索输入（防抖）携带 keyword 并重置到第 1 页', async () => {
    const wrapper = mountDrawer(true, [poolNode], 42)
    await flushPromises()

    // 先翻到第 2 页,验证搜索会把页码重置回 1
    await wrapper.find('.pg-next').trigger('click')
    await flushPromises()
    expect(nodesCalls().at(-1)![1]).toEqual({
      params: { page: 2, page_size: 10, source: '极速机场' }
    })

    await wrapper.find('input.el-input-stub').setValue('日本')
    // 等待防抖窗口(300ms)结束
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()
    expect(nodesCalls().at(-1)![1]).toEqual({
      params: { page: 1, page_size: 10, source: '极速机场', keyword: '日本' }
    })
  })

  it('防抖窗口内连续输入只发一次搜索请求', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()
    const before = nodesCalls().length

    const input = wrapper.find('input.el-input-stub')
    await input.setValue('日')
    await new Promise((r) => setTimeout(r, 100))
    await input.setValue('日本')
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()

    const after = nodesCalls()
    expect(after.length).toBe(before + 1)
    expect(after.at(-1)![1]).toEqual({
      params: { page: 1, page_size: 10, source: '极速机场', keyword: '日本' }
    })
  })

  it('清空搜索词后请求不再携带 keyword', async () => {
    const wrapper = mountDrawer(true)
    await flushPromises()

    const input = wrapper.find('input.el-input-stub')
    await input.setValue('JP')
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()
    expect(nodesCalls().at(-1)![1]).toEqual({
      params: { page: 1, page_size: 10, source: '极速机场', keyword: 'JP' }
    })

    await input.setValue('')
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()
    expect(nodesCalls().at(-1)![1]).toEqual({
      params: { page: 1, page_size: 10, source: '极速机场' }
    })
  })

  it('搜索无结果时展示匹配空态（不白屏）', async () => {
    const wrapper = mountDrawer(true, [])
    await flushPromises()

    await wrapper.find('input.el-input-stub').setValue('不存在')
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()
    expect(wrapper.text()).toContain('未找到匹配「不存在」的节点')
  })

  it('节点请求失败时降级为空态，不白屏', async () => {
    vi.mocked(client.get).mockImplementation(async (url: unknown) => {
      if (url === '/nodes') throw new Error('network down')
      if (url === '/airports/7/test/runs') return [] as never
      return {} as never
    })
    const wrapper = mount(AirportDetailDrawer, {
      props: { modelValue: true, airport },
      global: {
        directives: { loading: {} },
        stubs: {
          'el-drawer': ElDrawerStub,
          'el-descriptions': ElDescriptionsStub,
          'el-descriptions-item': ElDescriptionsItemStub,
          'el-table': ElTableStub,
          'el-table-column': ElTableColumnStub,
          'el-button': ElButtonStub,
          'el-tag': ElTagStub,
          'el-input': ElInputStub,
          'el-pagination': ElPaginationStub,
          'el-dropdown': ElDropdownStub,
          'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
          'el-dropdown-item': ElDropdownItemStub,
          'el-icon': SimpleSlotStub('ElIcon')
        }
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('该机场当前在池内无节点')
  })

  it('最近测试段：拉取 runs 传给报告组件；重跑意图上抛 {airport, full}', async () => {
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

  it('reloadReport 暴露给父级：重跑完成后重新拉取测试记录', async () => {
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
