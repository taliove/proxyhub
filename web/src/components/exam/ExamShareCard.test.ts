import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ExamShareCard from './ExamShareCard.vue'
import type { ExamReport } from '@/types'

const mockReport: ExamReport = {
  stability: {
    score: 88,
    total: 100,
    succeeded: 88,
    loss_rate: 0.12,
    mean_ms: 50,
    median_ms: 45,
    p95_ms: 80,
    p99_ms: 100,
    jitter_ms: 10
  },
  region_speed: {
    regions: [
      { code: 'x', name: '基准', ttfb_ms: 10, down_mbps: 500, up_mbps: 100 },
      { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80, up_mbps: 20 },
      { code: 'jp', name: '东京', ttfb_ms: 60, down_mbps: 150, up_mbps: 30 }
    ]
  },
  unlock: {
    results: [
      { node_key: 'k', target_name: 'Netflix', available: true, latency: 1, level: 'full' },
      { node_key: 'k', target_name: 'OpenAI', available: false, latency: 1, level: 'blocked' }
    ]
  },
  egress: {
    ipv4: {
      ip: '203.0.113.7',
      country: '美国',
      region: '加州',
      city: '洛杉矶',
      asn: 'AS64500',
      org: 'Example Hosting',
      proxy: false,
      hosting: true
    },
    dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
  }
}

describe('ExamShareCard', () => {
  it('默认(showAll=false):不渲染任何 IP/服务器地址,显示打码节点名与最佳/最差,含稳定性明细', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: '233boy-grpc-host',
        nodeServer: '1.2.3.4',
        examTime: new Date(2026, 6, 20, 9, 5)
      }
    })
    const html = wrapper.html()
    // 节点名打码
    expect(html).toContain('233boy-grpc-***')
    expect(html).not.toContain('233boy-grpc-host')
    // 无任何 IP/ASN
    expect(html).not.toContain('203.0.113.7')
    expect(html).not.toContain('1.2.3.4')
    expect(html).not.toContain('8.8.8.8')
    expect(html).not.toContain('AS64500')
    // 出口地区应显示
    expect(html).toContain('美国 · 加州 · 洛杉矶')
    expect(html).toContain('未泄露')
    // 多地域显示最佳/最差
    expect(html).toContain('最佳')
    expect(html).toContain('最差')
    expect(html).toContain('东京')
    expect(html).toContain('美西')
    // 无完整表格(列标题)
    expect(html).not.toContain('区域')
    expect(html).not.toContain('share-region-table')
    // 默认摘要版也渲染稳定性明细
    expect(html).toContain('稳定性指标')
    expect(html).toContain('丢包率')
    expect(html).toContain('12.0%')
    expect(html).toContain('平均延迟')
    expect(html).toContain('50 ms')
  })

  it('showAll=true:渲染完整节点名、全 IP、多地域全行表格、稳定性明细、出网全字段', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: '233boy-grpc-host',
        nodeServer: '1.2.3.4',
        examTime: new Date(2026, 6, 20, 9, 5),
        showAll: true
      }
    })
    const html = wrapper.html()
    // 完整节点名
    expect(html).toContain('233boy-grpc-host')
    expect(html).not.toContain('233boy-grpc-***')
    // 全 IP/ASN
    expect(html).toContain('203.0.113.7')
    expect(html).toContain('1.2.3.4')
    expect(html).toContain('8.8.8.8')
    expect(html).toContain('AS64500')
    expect(html).toContain('Example Hosting')
    // 多地域全行表格
    expect(html).toContain('区域')
    expect(html).toContain('延迟')
    expect(html).toContain('下行')
    expect(html).toContain('上行')
    expect(html).toContain('美西')
    expect(html).toContain('东京')
    // 表格中不含基准行(虽然"基准"出现在静态文本"Cloudflare 最近节点"中)
    const tableMatch = html.match(/<div[^>]*class="share-region-table"[^>]*>([\s\S]*?)<\/div>/)?.[1]
    expect(tableMatch).toBeDefined()
    expect(tableMatch).not.toContain('基准')
    // 稳定性明细
    expect(html).toContain('稳定性指标')
    expect(html).toContain('丢包率')
    expect(html).toContain('12.0%')
    expect(html).toContain('平均延迟')
    expect(html).toContain('50 ms')
    expect(html).toContain('中位延迟')
    expect(html).toContain('45 ms')
    expect(html).toContain('P95')
    expect(html).toContain('80 ms')
    expect(html).toContain('P99')
    expect(html).toContain('100 ms')
    expect(html).toContain('抖动')
    expect(html).toContain('10 ms')
    // 出网全字段
    expect(html).toContain('代理')
    expect(html).toContain('机房')
  })

  it('showAll=true 时卡片宽度增加', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date(),
        showAll: true
      }
    })
    expect(wrapper.find('.share-card-full').exists()).toBe(true)
  })

  it('评分环使用显式背景色(不依赖 color-mix)', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date()
      }
    })
    expect(wrapper.html()).not.toContain('color-mix')
  })

  it('评分环关键视觉属性内联为 presentation attributes(导出 PNG 不依赖样式表)', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date()
      }
    })
    const track = wrapper.find('.share-ring-track')
    const arc = wrapper.find('.share-ring-arc')
    expect(track.exists()).toBe(true)
    expect(arc.exists()).toBe(true)
    // 轨道:fill=none(防默认黑填充)+ stroke + stroke-width 内联
    expect(track.attributes('fill')).toBe('none')
    expect(track.attributes('stroke')).toBeTruthy()
    expect(track.attributes('stroke-width')).toBe('10')
    // 弧:fill=none + stroke(评分色)+ stroke-width + 圆头 内联
    expect(arc.attributes('fill')).toBe('none')
    expect(arc.attributes('stroke')).toBeTruthy()
    expect(arc.attributes('stroke-width')).toBe('10')
    expect(arc.attributes('stroke-linecap')).toBe('round')
    expect(arc.attributes('stroke-dasharray')).toBeTruthy()
    expect(arc.attributes('stroke-dashoffset')).toBeTruthy()
  })

  it('全量版多地域表:数值单元格单行不折行(nowrap)', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date(),
        showAll: true
      }
    })
    // 数值列(延迟/下行/上行)在渲染的表体中存在
    const table = wrapper.find('.share-region-table')
    expect(table.exists()).toBe(true)
    expect(table.findAll('.share-region-col-down').length).toBeGreaterThan(0)
    expect(table.findAll('.share-region-col-up').length).toBeGreaterThan(0)
    expect(table.findAll('.share-region-col-latency').length).toBeGreaterThan(0)
    // 值文本完整渲染(不因换行被拆碎)
    expect(table.html()).toContain('80.0 Mbps')
    expect(table.html()).toContain('150.0 Mbps')
  })

  it('无稳定性数据时:不渲染稳定性明细区块', () => {
    const reportWithoutStability = { ...mockReport, stability: undefined }
    const wrapper = mount(ExamShareCard, {
      props: {
        report: reportWithoutStability,
        nodeName: 'test',
        examTime: new Date()
      }
    })
    const html = wrapper.html()
    expect(html).not.toContain('稳定性指标')
    expect(html).not.toContain('丢包率')
  })
})
