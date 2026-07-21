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
      { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80 },
      { code: 'jp', name: '东京', ttfb_ms: 60, down_mbps: 150 }
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
      proxy: false,
      hosting: true
    },
    dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
  }
}

describe('ExamShareCard', () => {
  it('默认不渲染任何 IP/服务器地址', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: '233boy-grpc-host',
        nodeServer: '1.2.3.4',
        examTime: new Date(2026, 6, 20, 9, 5)
      }
    })
    const html = wrapper.html()
    expect(html).not.toContain('203.0.113.7') // 出口 IP
    expect(html).not.toContain('1.2.3.4') // 入口 IP
    expect(html).not.toContain('8.8.8.8') // DNS 解析器
    expect(html).toContain('美国 · 加州 · 洛杉矶') // 出口地区应显示
    expect(html).toContain('未泄露') // DNS 泄露状态应显示
  })

  it('showEgressIp=true 时渲染出口 IP', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date(),
        showEgressIp: true
      }
    })
    expect(wrapper.html()).toContain('203.0.113.7')
    expect(wrapper.html()).toContain('出口 IP')
  })

  it('showIngressIp=true 时渲染入口 IP', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        nodeServer: '1.2.3.4',
        examTime: new Date(),
        showIngressIp: true
      }
    })
    expect(wrapper.html()).toContain('1.2.3.4')
    expect(wrapper.html()).toContain('入口 IP')
  })

  it('showDns=true 时渲染 DNS 解析器', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        examTime: new Date(),
        showDns: true
      }
    })
    expect(wrapper.html()).toContain('8.8.8.8')
    expect(wrapper.html()).toContain('解析器')
  })

  it('三个开关全开:同时渲染三个地址字段', () => {
    const wrapper = mount(ExamShareCard, {
      props: {
        report: mockReport,
        nodeName: 'test',
        nodeServer: '1.2.3.4',
        examTime: new Date(),
        showEgressIp: true,
        showIngressIp: true,
        showDns: true
      }
    })
    const html = wrapper.html()
    expect(html).toContain('203.0.113.7')
    expect(html).toContain('1.2.3.4')
    expect(html).toContain('8.8.8.8')
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
})
