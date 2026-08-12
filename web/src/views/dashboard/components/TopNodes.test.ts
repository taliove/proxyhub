// TopNodes 组件测试:算分排序、Top10 截断、并列次序、标签收敛、协议门控、空态引导与降级
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TopNodes from './TopNodes.vue'
import client from '@/api/client'
import { ElMessage } from 'element-plus'
import type { ExamReport } from '@/types'

vi.mock('@/api/client')

// 构造完整四段体检报告:速度固定 100 分(基准 100Mbps)、出网干净 100 分,
// 总分 = 0.4*stability + 0.2*unlock + 40(unlock full=100 / blocked=0)
const makeReport = (stability: number, unlockLevel: 'full' | 'blocked' = 'full'): ExamReport => ({
  stability: {
    total: 10,
    succeeded: 10,
    loss_rate: 0,
    mean_ms: 50,
    median_ms: 50,
    p95_ms: 60,
    p99_ms: 70,
    jitter_ms: 5,
    score: stability
  },
  region_speed: {
    regions: [{ code: 'BASE', name: '基准', ttfb_ms: 10, down_mbps: 100, up_mbps: 50 }]
  },
  unlock: {
    results: [
      {
        node_key: 'k',
        target_name: 'Netflix',
        available: unlockLevel === 'full',
        latency: 100,
        level: unlockLevel
      }
    ]
  },
  egress: {
    ipv4: { proxy: false, hosting: false },
    ipv6: { available: true },
    dns: { leak: false }
  }
})

interface EntryOverrides {
  report?: ExamReport
  tags?: string[]
  type?: string
  available?: boolean
  source?: string
}

const makeEntry = (
  nodeKey: string,
  region: string,
  stability: number,
  overrides: EntryOverrides = {}
) => ({
  node_key: nodeKey,
  report: overrides.report ?? makeReport(stability),
  tags: overrides.tags ?? [],
  type: overrides.type ?? 'vmess',
  region,
  source: overrides.source ?? '机场A',
  available: overrides.available ?? true
})

// mock top-nodes 接口;值设为 Error 表示该路失败。share-uri 统一返回固定链接。
const mockApi = (entries: unknown) => {
  vi.mocked(client.get).mockImplementation((url: string) => {
    if (url.endsWith('/share-uri')) return Promise.resolve({ uri: 'vmess://share-uri' }) as never
    if (url !== '/dashboard/top-nodes')
      return Promise.reject(new Error(`unexpected url: ${url}`)) as never
    return (entries instanceof Error ? Promise.reject(entries) : Promise.resolve(entries)) as never
  })
}

const qrShow = vi.fn()
const writeText = vi.fn().mockResolvedValue(undefined)

const mountTopNodes = () =>
  mount(TopNodes, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        ElTag: { template: '<span class="el-tag"><slot /></span>' },
        ElButton: {
          template: '<button class="el-button" @click="$emit(\'click\')"><slot /></button>'
        },
        QRCodeDialog: {
          template: '<div class="qr-dialog" />',
          methods: { show: (uri: string) => qrShow(uri) }
        },
        RouterLink: {
          props: ['to'],
          template: '<a class="router-link" :href="to"><slot /></a>'
        }
      }
    }
  })

const regionOrder = (wrapper: ReturnType<typeof mountTopNodes>) =>
  wrapper.findAll('.item-region').map((el) => el.text())

describe('TopNodes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    Object.assign(navigator, { clipboard: { writeText } })
  })

  it('按算分总分降序渲染，展示地区/分数/档位/来源', async () => {
    mockApi([
      makeEntry('b:2', '日本', 50), // 总分 80 良好
      makeEntry('a:1', '香港', 100), // 总分 100 极好
      makeEntry('c:3', '美国', 25) // 总分 70 一般
    ])
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/dashboard/top-nodes')
    expect(regionOrder(wrapper)).toEqual(['香港', '日本', '美国'])

    const scores = wrapper.findAll('.item-score')
    expect(scores[0].text()).toContain('100')
    expect(scores[0].text()).toContain('极好')
    expect(scores[0].classes()).toContain('grade-excellent')
    expect(scores[1].text()).toContain('80')
    expect(scores[1].text()).toContain('良好')
    expect(scores[1].classes()).toContain('grade-good')
    expect(scores[2].text()).toContain('70')
    expect(scores[2].text()).toContain('一般')
    expect(scores[2].classes()).toContain('grade-fair')

    expect(wrapper.text()).toContain('机场A')
  })

  it('Top 10 截断：第 11 名及以后不上榜', async () => {
    // 12 条:稳定性 100..,其中最后两条分数最低应被截断
    const entries = Array.from({ length: 12 }, (_, i) => makeEntry(`n${i}:1`, `R${i}`, 100 - i * 5))
    mockApi(entries)
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(wrapper.findAll('.node-item')).toHaveLength(10)
    // 分数最低的两条(R10/R11)被截断
    expect(wrapper.text()).not.toContain('R10')
    expect(wrapper.text()).not.toContain('R11')
    // 榜首是分数最高的 R0
    expect(regionOrder(wrapper)[0]).toBe('R0')
  })

  it('总分并列时稳定性分高者在前，再并列保持接口顺序', async () => {
    // A:稳定性 100 + 解锁 blocked -> 总分 80,稳定性 100
    // B/C:稳定性 50 + 解锁 full -> 总分 80,稳定性 50(接口顺序 B 在 C 前)
    mockApi([
      makeEntry('b:1', '乙', 50),
      makeEntry('a:1', '甲', 100, { report: makeReport(100, 'blocked') }),
      makeEntry('c:1', '丙', 50)
    ])
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(regionOrder(wrapper)).toEqual(['甲', '乙', '丙'])
  })

  it('标签按优先级收敛为 3 个 chips + "+N"', async () => {
    mockApi([
      makeEntry('a:1', '香港', 100, {
        tags: ['region:US', 'dns-leak', 'stable-good', 'nf-full', 'fast']
      })
    ])
    const wrapper = mountTopNodes()
    await flushPromises()

    const chips = wrapper.findAll('.tag-chip').map((el) => el.text())
    // fast > 解锁类 > stable-* > 其他
    expect(chips).toEqual(['高速', 'Netflix全解', '稳定·优'])
    expect(wrapper.find('.tags-overflow').text()).toBe('+2')
  })

  it('标签不超过 3 个时不显示 "+N"', async () => {
    mockApi([makeEntry('a:1', '香港', 100, { tags: ['fast', 'ipv6'] })])
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(wrapper.findAll('.tag-chip')).toHaveLength(2)
    expect(wrapper.find('.tags-overflow').exists()).toBe(false)
  })

  it('协议不支持时隐藏分享操作，支持的协议显示并可复制', async () => {
    mockApi([makeEntry('a:1', '香港', 100, { type: 'hysteria2' }), makeEntry('b:2', '日本', 50)])
    const wrapper = mountTopNodes()
    await flushPromises()

    const rows = wrapper.findAll('.node-item')
    expect(rows[0].findAll('button')).toHaveLength(0)

    const ops = rows[1].findAll('button')
    expect(ops.map((b) => b.text())).toEqual(['复制链接', '二维码'])

    await ops[0].trigger('click')
    await flushPromises()
    expect(client.get).toHaveBeenCalledWith(`/nodes/${encodeURIComponent('b:2')}/share-uri`)
    expect(writeText).toHaveBeenCalledWith('vmess://share-uri')
    expect(ElMessage.success).toHaveBeenCalledWith('节点链接已复制到剪贴板')
  })

  it('点击二维码取分享链接并调起 QRCodeDialog', async () => {
    mockApi([makeEntry('a:1', '香港', 100)])
    const wrapper = mountTopNodes()
    await flushPromises()

    const qrButton = wrapper.findAll('button').find((b) => b.text() === '二维码')
    await qrButton!.trigger('click')
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith(`/nodes/${encodeURIComponent('a:1')}/share-uri`)
    expect(qrShow).toHaveBeenCalledWith('vmess://share-uri')
  })

  it('不可用节点视觉降级并带不可用标识', async () => {
    mockApi([makeEntry('a:1', '香港', 100, { available: false })])
    const wrapper = mountTopNodes()
    await flushPromises()

    const row = wrapper.find('.node-item')
    expect(row.classes()).toContain('is-unavailable')
    expect(row.find('.item-state').text()).toBe('不可用')
  })

  it('空列表渲染空态引导与跳转节点页链接', async () => {
    mockApi([])
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(wrapper.text()).toContain('还没有体检过的节点')
    const link = wrapper.find('.router-link')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('/nodes')
    expect(link.text()).toContain('去节点页跑体检')
  })

  it('请求未完成时显示加载中', () => {
    vi.mocked(client.get).mockReturnValue(new Promise(() => {}) as never)
    const wrapper = mountTopNodes()

    expect(wrapper.find('.panel-empty').text()).toBe('加载中……')
  })

  it('请求失败时降级为失败提示（全局拦截器已 toast）', async () => {
    mockApi(new Error('network'))
    const wrapper = mountTopNodes()
    await flushPromises()

    expect(wrapper.find('.panel-empty').text()).toContain('加载失败')
    expect(wrapper.find('.router-link').exists()).toBe(false)
  })
})
