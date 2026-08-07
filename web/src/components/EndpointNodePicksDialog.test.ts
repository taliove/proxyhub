import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, watch, type Ref } from 'vue'
import type { Endpoint, Node } from '@/types'
import EndpointNodePicksDialog from './EndpointNodePicksDialog.vue'
import { parseNodePicks, nodePicksCount, nodePicksLabel } from './endpoint-nodepicks-utils'
import client from '@/api/client'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

// el-dialog 桩:modelValue 变 true 时发 open 事件(组件凭此初始化),关闭态不渲染
const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  emits: ['update:modelValue', 'open'],
  setup(props, { slots, emit }) {
    watch(
      () => props.modelValue,
      (v) => {
        if (v) emit('open')
      },
      { immediate: true }
    )
    return () =>
      props.modelValue
        ? h('div', { class: 'el-dialog-stub' }, [slots.default?.(), slots.footer?.()])
        : null
  }
})

// el-table/el-table-column 桩:列按行渲染 scoped slot(照 Endpoints.test.ts 形制)
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
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-input-inner',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
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

// fixture 全合成:example.com + 全零 UUID 风格(issue #80)
const poolNodes: Node[] = [
  {
    name: '香港 01',
    display_name: 'HK-01',
    type: 'ss',
    server: 'hk1.example.com',
    port: 443,
    tls: true,
    region: 'HK',
    source: '机场A',
    latency: 50,
    available: true,
    node_key: 'hk1.example.com:443',
    blocked: false,
    stale: false,
    availability_source: 'real'
  },
  {
    name: '美国 01',
    display_name: 'US-01',
    type: 'trojan',
    server: 'us1.example.com',
    port: 8443,
    tls: true,
    sni: 'cdn.example.com',
    region: 'US',
    source: '机场B',
    latency: 160,
    available: true,
    node_key: 'us1.example.com:8443:cdn.example.com',
    blocked: false,
    stale: false,
    availability_source: 'health'
  }
]

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
  node_picks: ''
}

const mountDialog = (props: {
  endpoint: Endpoint | null
  stagedPicks?: string[]
  modelValue?: boolean
}) => {
  vi.mocked(client.get).mockResolvedValue({ nodes: poolNodes, total: 2 } as never)
  return mount(EndpointNodePicksDialog, {
    props: { modelValue: props.modelValue ?? true, ...props },
    global: {
      directives: { loading: {} },
      stubs: {
        'el-dialog': ElDialogStub,
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-input': ElInputStub,
        'el-button': ElButtonStub,
        'el-tag': ElTagStub
      }
    }
  })
}

describe('parseNodePicks/nodePicksLabel 纯函数(issue #80)', () => {
  it('空串/缺省/非法 JSON 均按空集(与后端降级语义一致)', () => {
    expect(parseNodePicks('')).toEqual([])
    expect(parseNodePicks(undefined)).toEqual([])
    expect(parseNodePicks('not-json')).toEqual([])
    expect(parseNodePicks('{"x":1}')).toEqual([])
  })

  it('合法 JSON 数组原样返回;非字符串元素被过滤', () => {
    expect(parseNodePicks('["a.example.com:443","b.example.com:8443:sni"]')).toEqual([
      'a.example.com:443',
      'b.example.com:8443:sni'
    ])
    expect(parseNodePicks('["a.example.com:443",1]')).toEqual(['a.example.com:443'])
  })

  it('数量与文案:0=全量,非零=精选 N 个节点', () => {
    expect(nodePicksCount({})).toBe(0)
    expect(nodePicksLabel({})).toBe('全量')
    expect(nodePicksLabel({ node_picks: '["a:1","b:2","c:3"]' })).toBe('精选 3 个节点')
    expect(nodePicksLabel({ node_picks: 'bad' })).toBe('全量')
  })
})

describe('EndpointNodePicksDialog(issue #80)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('打开时拉取全量池节点(大 page_size,同 useNodePool)', async () => {
    mountDialog({ endpoint })
    await flushPromises()
    expect(vi.mocked(client.get)).toHaveBeenCalledWith('/nodes', {
      params: { page: 1, page_size: 100000 }
    })
  })

  it('编辑模式回显已选;已消失的 key 标「已失效」且保存时保留', async () => {
    const ep: Endpoint = {
      ...endpoint,
      node_picks: '["hk1.example.com:443","gone.example.com:443"]'
    }
    const wrapper = mountDialog({ endpoint: ep })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('HK-01') // 命中池节点显示名称
    expect(text).toContain('已失效')
    expect(text).toContain('gone.example.com:443')
    expect(text).toContain('已选 2 个')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '确定')!
      .trigger('click')
    await flushPromises()

    // 失效 key 保留在配置里(复活自动恢复,后端语义)
    expect(vi.mocked(client.put)).toHaveBeenCalledWith('/endpoints/7/node-picks', {
      node_picks: ['hk1.example.com:443', 'gone.example.com:443']
    })
    expect(wrapper.emitted('saved')).toBeTruthy()
  })

  it('从池中添加节点后保存,NodeKey 入精选数组', async () => {
    const wrapper = mountDialog({ endpoint })
    await flushPromises()

    const addButtons = wrapper.findAll('button').filter((b) => b.text() === '添加')
    expect(addButtons.length).toBe(2)
    await addButtons[1].trigger('click') // US-01(带 SNI 的 NodeKey)
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '确定')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.put)).toHaveBeenCalledWith('/endpoints/7/node-picks', {
      node_picks: ['us1.example.com:8443:cdn.example.com']
    })
  })

  it('移除全部已选后保存为空数组(清空精选 = 恢复全量)', async () => {
    const ep: Endpoint = { ...endpoint, node_picks: '["hk1.example.com:443"]' }
    const wrapper = mountDialog({ endpoint: ep })
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((b) => b.text() === '移除')!
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '确定')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.put)).toHaveBeenCalledWith('/endpoints/7/node-picks', {
      node_picks: []
    })
  })

  it('关键词过滤池列表(名称/来源/地区子串,大小写不敏感)', async () => {
    const wrapper = mountDialog({ endpoint })
    await flushPromises()
    expect(wrapper.findAll('button').filter((b) => b.text() === '添加').length).toBe(2)

    await wrapper.find('input.el-input-inner').setValue('机场b')
    await flushPromises()
    const addButtons = wrapper.findAll('button').filter((b) => b.text() === '添加')
    expect(addButtons.length).toBe(1)
    expect(wrapper.text()).toContain('US-01')
    expect(wrapper.text()).not.toContain('HK-01')
  })

  it('新建暂存模式(endpoint=null):确定上抛 confirm,不发 PUT', async () => {
    const wrapper = mountDialog({ endpoint: null, stagedPicks: ['hk1.example.com:443'] })
    await flushPromises()

    expect(wrapper.text()).toContain('已选 1 个') // 暂存回显

    await wrapper
      .findAll('button')
      .filter((b) => b.text() === '添加')[0]
      .trigger('click') // 添加 HK-01(重复,应去重)
    await wrapper
      .findAll('button')
      .filter((b) => b.text() === '添加')[1]
      .trigger('click') // 添加 US-01
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '确定')!
      .trigger('click')
    await flushPromises()

    expect(vi.mocked(client.put)).not.toHaveBeenCalled()
    expect(wrapper.emitted('confirm')).toEqual([
      [['hk1.example.com:443', 'us1.example.com:8443:cdn.example.com']]
    ])
  })
})
