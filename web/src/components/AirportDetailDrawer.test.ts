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
  vi.mocked(client.get).mockResolvedValue({ nodes: nodeList } as never)
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

  it('打开抽屉只按机场名拉取池快照,不触发任何测试/检活请求', async () => {
    mountDrawer(true)
    await flushPromises()

    const getCalls = vi.mocked(client.get).mock.calls
    expect(getCalls).toHaveLength(1)
    expect(getCalls[0][0]).toBe('/nodes')
    expect(getCalls[0][1]).toMatchObject({
      params: { source: '极速机场' }
    })
    // 无任何 POST(测试/检活/体检都是 POST/SSE)
    expect(vi.mocked(client.post)).not.toHaveBeenCalled()
    // 没有任何指向测试/检测类端点的 GET
    for (const [url] of getCalls) {
      expect(String(url)).not.toMatch(/test|detect|exam|check/i)
    }
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
})
