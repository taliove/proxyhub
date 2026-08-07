// QuickEndpoints 组件测试:URL 拼装、复制调用、二维码展示与空态引导
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import QuickEndpoints from './QuickEndpoints.vue'
import client from '@/api/client'
import { ElMessage } from 'element-plus'

vi.mock('@/api/client')

const makeEndpoint = (id: number, alias: string, path: string, token: string) => ({
  id,
  alias,
  path,
  token,
  enabled: true,
  created_at: '2026-07-01 00:00:00',
  name_mode: '' as const,
  name_template: '',
  conditions: ''
})

const endpointsPayload = [
  makeEndpoint(1, '老爸的手机', 'abc123', 'tok-1'),
  makeEndpoint(2, 'iPad', 'def456', 'tok-2')
]

const qrShow = vi.fn()
const writeText = vi.fn().mockResolvedValue(undefined)

const mountQuickEndpoints = () =>
  mount(QuickEndpoints, {
    global: {
      stubs: {
        ElCard: { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        ElButton: {
          template: '<button class="el-button" @click="$emit(\'click\')"><slot /></button>'
        },
        QRCodeDialog: {
          template: '<div class="qr-dialog" />',
          methods: { show: (url: string) => qrShow(url) }
        },
        RouterLink: {
          props: ['to'],
          template: '<a class="router-link" :href="to"><slot /></a>'
        }
      }
    }
  })

describe('QuickEndpoints', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    Object.assign(navigator, { clipboard: { writeText } })
  })

  it('渲染别名与拼装的完整订阅 URL', async () => {
    vi.mocked(client.get).mockResolvedValue(endpointsPayload)
    const wrapper = mountQuickEndpoints()
    await flushPromises()

    expect(client.get).toHaveBeenCalledWith('/endpoints')
    const origin = window.location.origin
    const text = wrapper.text()
    expect(text).toContain('老爸的手机')
    expect(text).toContain(`${origin}/sub/abc123?token=tok-1`)
    expect(text).toContain('iPad')
    expect(text).toContain(`${origin}/sub/def456?token=tok-2`)
  })

  it('点击复制写入剪贴板并提示成功', async () => {
    vi.mocked(client.get).mockResolvedValue(endpointsPayload)
    const wrapper = mountQuickEndpoints()
    await flushPromises()

    const copyButtons = wrapper.findAll('button').filter((btn) => btn.text() === '复制')
    expect(copyButtons.length).toBe(2)
    await copyButtons[0].trigger('click')

    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/sub/abc123?token=tok-1`)
    expect(ElMessage.success).toHaveBeenCalledWith('已复制到剪贴板')
  })

  it('点击二维码用完整 URL 调起 QRCodeDialog', async () => {
    vi.mocked(client.get).mockResolvedValue(endpointsPayload)
    const wrapper = mountQuickEndpoints()
    await flushPromises()

    const qrButtons = wrapper.findAll('button').filter((btn) => btn.text() === '二维码')
    expect(qrButtons.length).toBe(2)
    await qrButtons[1].trigger('click')

    expect(qrShow).toHaveBeenCalledWith(`${window.location.origin}/sub/def456?token=tok-2`)
  })

  it('空列表渲染引导文案与跳转链接', async () => {
    vi.mocked(client.get).mockResolvedValue([])
    const wrapper = mountQuickEndpoints()
    await flushPromises()

    expect(wrapper.text()).toContain('还没有订阅地址')
    const link = wrapper.find('.router-link')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('/endpoints')
  })

  it('请求失败时降级为失败提示（全局拦截器已 toast）', async () => {
    vi.mocked(client.get).mockRejectedValue(new Error('network'))
    const wrapper = mountQuickEndpoints()
    await flushPromises()

    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.find('.router-link').exists()).toBe(false)
  })
})
