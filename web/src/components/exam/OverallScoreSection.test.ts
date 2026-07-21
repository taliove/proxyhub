// OverallScoreSection 组件测试:渐进式评分展示(进行中显示实时分数,不再显示"—")
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import OverallScoreSection from './OverallScoreSection.vue'
import type { ExamReport } from '@/types'

describe('OverallScoreSection — 综合评分展示', () => {
  it('进行中:只有稳定性段 → 显示 0 分(缺出网段 unreliable)', () => {
    const report: ExamReport = {
      stability: { score: 80 } as any
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: false }
    })

    // 缺出网段时 unreliable=true,显示 0 分
    expect(wrapper.find('.overall-score-ring-score').text()).toBe('0')
    // 应有"不可信"标记(优先于"部分数据")
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(true)
    expect(wrapper.find('.overall-score-unreliable').text()).toBe('不可信')
  })

  it('进行中:稳定性 + 速度 → 显示 0 分(缺出网段 unreliable)', () => {
    const report: ExamReport = {
      stability: { score: 85 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 50, up_mbps: 50, ttfb_ms: 10 }]
      }
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: false }
    })

    // 缺出网段 → unreliable
    expect(wrapper.find('.overall-score-ring-score').text()).toBe('0')
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(true)
  })

  it('进行中:缺出网段 → unreliable 标记优先于 partial', () => {
    const report: ExamReport = {
      stability: { score: 90 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 100, ttfb_ms: 5 }]
      },
      unlock: {
        results: [
          { target_name: 'Netflix', level: 'full' },
          { target_name: 'YouTube Premium', level: 'full' },
          { target_name: 'Disney+', level: 'full' },
          { target_name: 'OpenAI', level: 'full' },
          { target_name: 'Claude', level: 'full' },
          { target_name: 'Gemini', level: 'full' }
        ] as any[]
      }
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: false }
    })

    // 缺出网段时标记 unreliable
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(true)
    expect(wrapper.find('.overall-score-unreliable').text()).toBe('不可信')
    expect(wrapper.find('.overall-score-partial').exists()).toBe(false)
  })

  it('进行中:出网全失败 → 强制显示 0 分 + 不可信标记', () => {
    const report: ExamReport = {
      stability: { score: 90 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 100, ttfb_ms: 5 }]
      },
      egress: {
        ipv4: { error: 'timeout' },
        ipv6: { available: false, error: 'timeout' },
        dns: { error: 'timeout' }
      } as any
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: false }
    })

    expect(wrapper.find('.overall-score-ring-score').text()).toBe('0')
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(true)
  })

  it('完成态:四段全到 → 显示完整分数,无 partial 标记', () => {
    const report: ExamReport = {
      stability: { score: 85 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 50, up_mbps: 50, ttfb_ms: 10 }]
      },
      unlock: {
        results: [
          { target_name: 'Netflix', level: 'full' },
          { target_name: 'YouTube Premium', level: 'full' },
          { target_name: 'Disney+', level: 'full' },
          { target_name: 'OpenAI', level: 'full' },
          { target_name: 'Claude', level: 'full' },
          { target_name: 'Gemini', level: 'full' }
        ] as any[]
      },
      egress: {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: true, address: '2001::1' },
        dns: { leak: false }
      } as any
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: true }
    })

    // 完整四段:约 91-92 分
    const score = parseInt(wrapper.find('.overall-score-ring-score').text())
    expect(score).toBeGreaterThan(90)
    expect(score).toBeLessThan(93)
    // 无 partial 或 unreliable 标记
    expect(wrapper.find('.overall-score-partial').exists()).toBe(false)
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(false)
  })

  it('进行中:稳定性 + 速度 + 出网(缺解锁) → 渐进分数不归一化(缺段计 0)+ 部分数据标记', () => {
    const report: ExamReport = {
      stability: { score: 85 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 50, up_mbps: 50, ttfb_ms: 10 }]
      },
      egress: {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: true, address: '2001::1' },
        dns: { leak: false }
      } as any
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: false }
    })

    // 进行中(progressive):缺解锁段(权重 0.2)直接计 0,不归一化。
    // 稳定性 85×0.4 + 速度 90×0.25 + 出网 100×0.15 = 34 + 22.5 + 15 = 71.5 → 72。
    const score = parseInt(wrapper.find('.overall-score-ring-score').text())
    expect(score).toBe(72)
    // 缺解锁段,显示部分数据
    expect(wrapper.find('.overall-score-partial').exists()).toBe(true)
    expect(wrapper.find('.overall-score-unreliable').exists()).toBe(false)
  })

  it('进行中 vs 完成态:同一份缺解锁报告,progressive 低于 normalized(由小到大爬升语义)', () => {
    const report: ExamReport = {
      stability: { score: 85 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 50, up_mbps: 50, ttfb_ms: 10 }]
      },
      egress: {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: true, address: '2001::1' },
        dns: { leak: false }
      } as any
    }
    const inProgress = mount(OverallScoreSection, { props: { report, terminal: false } })
    const finished = mount(OverallScoreSection, { props: { report, terminal: true } })
    const progScore = parseInt(inProgress.find('.overall-score-ring-score').text())
    const normScore = parseInt(finished.find('.overall-score-ring-score').text())
    // 进行中缺段计 0(72),完成态归一化补齐到 ~89;progressive 严格更低。
    expect(progScore).toBeLessThan(normScore)
  })

  it('历史报告卡:缺解锁段但有出网 → 显示渐进分数', () => {
    const report: ExamReport = {
      stability: { score: 75 } as any,
      region_speed: {
        regions: [{ name: '基准', code: 'baseline', down_mbps: 25, ttfb_ms: 15 }]
      },
      egress: {
        ipv4: { ip: '1.2.3.4', hosting: false },
        ipv6: { available: false },
        dns: { leak: false }
      } as any
    }
    const wrapper = mount(OverallScoreSection, {
      props: { report, terminal: true }
    })

    // 三段归一化分数
    const score = parseInt(wrapper.find('.overall-score-ring-score').text())
    expect(score).toBeGreaterThan(70)
    expect(score).toBeLessThan(85)
  })
})
