// 体检总分纯函数的单元测试(加权四项:稳定性 40% + 速度 25% + 解锁 20% + 出网质量 15%)。
import { describe, it, expect } from 'vitest'
import {
  calculateExamScore,
  calculateSpeedScore,
  calculateUnlockScore,
  calculateEgressScore,
  gradeFromScore
} from './score'
import type { ExamReport } from '@/types'

describe('score — 体检总分计算', () => {
  describe('calculateSpeedScore', () => {
    it('基准下行对数映射边界值', () => {
      expect(calculateSpeedScore({ down_mbps: 100, up_mbps: undefined })).toBe(100)
      expect(calculateSpeedScore({ down_mbps: 50, up_mbps: undefined })).toBe(85)
      expect(calculateSpeedScore({ down_mbps: 25, up_mbps: undefined })).toBe(70)
      expect(calculateSpeedScore({ down_mbps: 10, up_mbps: undefined })).toBe(50)
      expect(calculateSpeedScore({ down_mbps: 5, up_mbps: undefined })).toBe(30)
      expect(calculateSpeedScore({ down_mbps: 1, up_mbps: undefined })).toBe(10)
    })

    it('基准下行中间插值', () => {
      // 50M 到 100M 之间线性插值:50M=85,100M=100,区间长度 15
      const score75 = calculateSpeedScore({ down_mbps: 75, up_mbps: undefined })
      expect(score75).toBeCloseTo(92.5, 1)

      // 10M 到 25M 之间:10M=50,25M=70,区间长度 20
      const score15 = calculateSpeedScore({ down_mbps: 15, up_mbps: undefined })
      expect(score15).toBeCloseTo(56.7, 1)
    })

    it('上行微调 ±5 分', () => {
      // 下行 50M(=85 分),上行 50M 再 +5 → 90
      expect(calculateSpeedScore({ down_mbps: 50, up_mbps: 50 })).toBe(90)

      // 下行 50M(=85 分),上行 5M 再 −5 → 80
      expect(calculateSpeedScore({ down_mbps: 50, up_mbps: 5 })).toBe(80)

      // 上行缺失时不微调
      expect(calculateSpeedScore({ down_mbps: 50, up_mbps: undefined })).toBe(85)
    })

    it('下行低于 1M 按 0 分计', () => {
      expect(calculateSpeedScore({ down_mbps: 0.5, up_mbps: undefined })).toBe(0)
      expect(calculateSpeedScore({ down_mbps: 0, up_mbps: undefined })).toBe(0)
    })

    it('下行高于 100M 封顶 100 分', () => {
      expect(calculateSpeedScore({ down_mbps: 150, up_mbps: undefined })).toBe(100)
      expect(calculateSpeedScore({ down_mbps: 500, up_mbps: 100 })).toBe(100) // +5 也不超 100
    })
  })

  describe('calculateUnlockScore', () => {
    it('6 目标全 full = 100 分', () => {
      const results = [
        { target_name: 'Netflix', level: 'full' },
        { target_name: 'YouTube Premium', level: 'full' },
        { target_name: 'Disney+', level: 'full' },
        { target_name: 'OpenAI', level: 'full' },
        { target_name: 'Claude', level: 'full' },
        { target_name: 'Gemini', level: 'full' }
      ] as any[]
      expect(calculateUnlockScore(results)).toBe(100)
    })

    it('6 目标全 originals_only = 50 分', () => {
      const results = [
        { target_name: 'Netflix', level: 'originals_only' },
        { target_name: 'YouTube Premium', level: 'originals_only' },
        { target_name: 'Disney+', level: 'originals_only' },
        { target_name: 'OpenAI', level: 'originals_only' },
        { target_name: 'Claude', level: 'originals_only' },
        { target_name: 'Gemini', level: 'originals_only' }
      ] as any[]
      expect(calculateUnlockScore(results)).toBe(50)
    })

    it('6 目标全 blocked/error = 0 分', () => {
      const results = [
        { target_name: 'Netflix', level: 'blocked' },
        { target_name: 'YouTube Premium', error: 'timeout' },
        { target_name: 'Disney+', level: 'blocked' },
        { target_name: 'OpenAI', error: 'network' },
        { target_name: 'Claude', level: 'blocked' },
        { target_name: 'Gemini', error: 'failed' }
      ] as any[]
      expect(calculateUnlockScore(results)).toBe(0)
    })

    it('混合档位取均值', () => {
      // 3 full(=1) + 2 originals_only(=0.5) + 1 blocked(=0) → (3+1)/6 = 66.67
      const results = [
        { target_name: 'Netflix', level: 'full' },
        { target_name: 'YouTube Premium', level: 'full' },
        { target_name: 'Disney+', level: 'full' },
        { target_name: 'OpenAI', level: 'originals_only' },
        { target_name: 'Claude', level: 'originals_only' },
        { target_name: 'Gemini', level: 'blocked' }
      ] as any[]
      expect(calculateUnlockScore(results)).toBeCloseTo(66.67, 1)
    })

    it('空数组返回 0 分', () => {
      expect(calculateUnlockScore([])).toBe(0)
    })
  })

  describe('calculateEgressScore', () => {
    it('完美出网:无泄露 + 住宅 IP + 有 IPv6 = 100 分', () => {
      const egress = {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: true, address: '2001::1' },
        dns: { leak: false }
      }
      expect(calculateEgressScore(egress as any)).toBe(100)
    })

    it('DNS 泄露 −30 分', () => {
      const egress = {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: true, address: '2001::1' },
        dns: { leak: true }
      }
      expect(calculateEgressScore(egress as any)).toBe(70)
    })

    it('机房 IP −15 分', () => {
      const egress = {
        ipv4: { ip: '1.2.3.4', hosting: true, proxy: false },
        ipv6: { available: true },
        dns: { leak: false }
      }
      expect(calculateEgressScore(egress as any)).toBe(85)
    })

    it('无 IPv6 −10 分', () => {
      const egress = {
        ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
        ipv6: { available: false },
        dns: { leak: false }
      }
      expect(calculateEgressScore(egress as any)).toBe(90)
    })

    it('叠加扣分:DNS 泄露 + 机房 + 无 IPv6 → 45 分', () => {
      const egress = {
        ipv4: { ip: '1.2.3.4', hosting: true, proxy: false },
        ipv6: { available: false },
        dns: { leak: true }
      }
      expect(calculateEgressScore(egress as any)).toBe(45)
    })

    it('保底 0 分', () => {
      const egress = {
        ipv4: { error: 'failed' },
        ipv6: { available: false },
        dns: { error: 'timeout' }
      }
      expect(calculateEgressScore(egress as any)).toBeGreaterThanOrEqual(0)
    })
  })

  describe('gradeFromScore', () => {
    it('档位边界', () => {
      expect(gradeFromScore(100)).toBe('excellent')
      expect(gradeFromScore(90)).toBe('excellent')
      expect(gradeFromScore(89)).toBe('good')
      expect(gradeFromScore(75)).toBe('good')
      expect(gradeFromScore(74)).toBe('fair')
      expect(gradeFromScore(60)).toBe('fair')
      expect(gradeFromScore(59)).toBe('poor')
      expect(gradeFromScore(40)).toBe('poor')
      expect(gradeFromScore(39)).toBe('very_poor')
      expect(gradeFromScore(0)).toBe('very_poor')
    })
  })

  describe('calculateExamScore — 加权汇总', () => {
    it('完整四段:稳定性 40% + 速度 25% + 解锁 20% + 出网 15% = 权重和 100%', () => {
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
      const result = calculateExamScore(report)
      // 稳定性 85×0.4=34,速度 90×0.25=22.5,解锁 100×0.2=20,出网 100×0.15=15 → 91.5
      expect(result.total).toBeCloseTo(91.5, 1)
      expect(result.grade).toBe('excellent')
      expect(result.partial).toBe(false)
      expect(result.breakdown.stability.score).toBe(85)
      expect(result.breakdown.stability.weight).toBe(0.4)
      expect(result.breakdown.speed.score).toBe(90)
      expect(result.breakdown.speed.weight).toBe(0.25)
      expect(result.breakdown.unlock.score).toBe(100)
      expect(result.breakdown.unlock.weight).toBe(0.2)
      expect(result.breakdown.egress.score).toBe(100)
      expect(result.breakdown.egress.weight).toBe(0.15)
    })

    it('缺段降级:无解锁段时三项归一化(权重和仍为 100%),partial=true', () => {
      const report: ExamReport = {
        stability: { score: 80 } as any,
        region_speed: {
          regions: [{ name: '基准', code: 'baseline', down_mbps: 100, ttfb_ms: 5 }]
        },
        egress: {
          ipv4: { ip: '1.2.3.4', hosting: false, proxy: false },
          ipv6: { available: true },
          dns: { leak: false }
        } as any
      }
      const result = calculateExamScore(report)
      // 原权重和 40%+25%+15%=80%,归一化:稳定性 40/80=0.5,速度 25/80=0.3125,出网 15/80=0.1875
      // 稳定性 80×0.5=40,速度 100×0.3125=31.25,出网 100×0.1875=18.75 → 90
      expect(result.total).toBeCloseTo(90, 1)
      expect(result.partial).toBe(true)
      expect(result.breakdown.stability.weight).toBeCloseTo(0.5, 2)
      expect(result.breakdown.speed.weight).toBeCloseTo(0.3125, 2)
      expect(result.breakdown.unlock.score).toBeNull()
      expect(result.breakdown.unlock.weight).toBe(0)
      expect(result.breakdown.egress.weight).toBeCloseTo(0.1875, 2)
    })

    it('只有稳定性段:权重归一化为 100%', () => {
      const report: ExamReport = {
        stability: { score: 90 } as any
      }
      const result = calculateExamScore(report)
      expect(result.total).toBe(90)
      expect(result.partial).toBe(true)
      expect(result.breakdown.stability.weight).toBe(1.0)
      expect(result.breakdown.speed.score).toBeNull()
      expect(result.breakdown.unlock.score).toBeNull()
      expect(result.breakdown.egress.score).toBeNull()
    })

    it('空报告返回 0 分', () => {
      const report: ExamReport = {}
      const result = calculateExamScore(report)
      expect(result.total).toBe(0)
      expect(result.grade).toBe('very_poor')
      expect(result.partial).toBe(true)
    })
  })

  describe('可信度标记(unreliable)', () => {
    it('出网全失败 → unreliable=true + 总分 0', () => {
      const report: ExamReport = {
        egress: {
          ipv4: { error: 'timeout' },
          ipv6: { available: false, error: 'timeout' },
          dns: { error: 'timeout' }
        } as any
      }
      const result = calculateExamScore(report)
      expect(result.unreliable).toBe(true)
      expect(result.total).toBe(0)
      expect(result.partial).toBe(true)
    })

    it('IPv6 不可达(非 error)但 IPv4/DNS 成功 → reliable', () => {
      const report: ExamReport = {
        egress: {
          ipv4: { ip: '203.0.113.7', country: 'US' },
          ipv6: { available: false }, // 不可达非失败
          dns: { resolver_ip: '8.8.8.8', resolver_geo: 'US' }
        } as any,
        stability: { score: 85 } as any
      }
      const result = calculateExamScore(report)
      expect(result.unreliable).toBe(false)
      expect(result.total).toBeGreaterThan(0)
    })

    it('缺出网段 → unreliable=true', () => {
      const report: ExamReport = {
        stability: { score: 85 } as any,
        region_speed: {
          regions: [{ name: '基准', code: 'baseline', down_mbps: 50, ttfb_ms: 10 }]
        }
      }
      const result = calculateExamScore(report)
      expect(result.unreliable).toBe(true)
      expect(result.partial).toBe(true)
    })

    it('有出网段且部分成功 → reliable', () => {
      const report: ExamReport = {
        egress: {
          ipv4: { ip: '203.0.113.7', country: 'US' },
          ipv6: { available: false },
          dns: { resolver_ip: '8.8.8.8', resolver_geo: 'US' }
        } as any,
        stability: { score: 85 } as any
      }
      const result = calculateExamScore(report)
      expect(result.unreliable).toBe(false)
    })

    it('完整报告 → reliable', () => {
      const report: ExamReport = {
        egress: {
          ipv4: { ip: '203.0.113.7', country: 'US', hosting: false },
          ipv6: { available: true, address: '2001:db8::1' },
          dns: { resolver_ip: '8.8.8.8', resolver_geo: 'US', leak: false }
        } as any,
        stability: { score: 85 } as any,
        region_speed: {
          regions: [{ name: '基准', code: 'baseline', down_mbps: 50, ttfb_ms: 10 }]
        },
        unlock: {
          results: [{ target_name: 'Netflix', level: 'full' }] as any[]
        }
      }
      const result = calculateExamScore(report)
      expect(result.unreliable).toBe(false)
      expect(result.partial).toBe(false)
    })
  })

  describe('渐进式评分(体检进行中)', () => {
    it('只有稳定性段到达 → 按稳定性分算总分,标记 partial', () => {
      const report: ExamReport = {
        stability: { score: 80 } as any
      }
      const result = calculateExamScore(report)
      // 稳定性权重归一化为 100%
      expect(result.total).toBe(80)
      expect(result.partial).toBe(true)
      expect(result.breakdown.stability.weight).toBe(1.0)
      expect(result.breakdown.stability.score).toBe(80)
    })

    it('稳定性 + 速度到达 → 两项归一化权重,分数增长', () => {
      const report: ExamReport = {
        stability: { score: 85 } as any,
        region_speed: {
          regions: [{ name: '基准', code: 'baseline', down_mbps: 50, up_mbps: 50, ttfb_ms: 10 }]
        }
      }
      const result = calculateExamScore(report)
      // 稳定性 40% + 速度 25% = 65%,归一化:稳定性 40/65,速度 25/65
      // 稳定性 85×(40/65) + 速度 90×(25/65) = 52.3 + 34.6 = 86.9
      expect(result.total).toBeCloseTo(86.9, 1)
      expect(result.partial).toBe(true)
      expect(result.breakdown.stability.weight).toBeCloseTo(0.615, 2)
      expect(result.breakdown.speed.weight).toBeCloseTo(0.385, 2)
    })

    it('稳定性 + 速度 + 解锁到达 → 三项归一化,分数继续增长', () => {
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
        }
      }
      const result = calculateExamScore(report)
      // 稳定性 40% + 速度 25% + 解锁 20% = 85%,归一化
      // 稳定性 85×(40/85) + 速度 90×(25/85) + 解锁 100×(20/85) = 40 + 26.5 + 23.5 = 90
      expect(result.total).toBeCloseTo(90, 1)
      expect(result.partial).toBe(true)
      expect(result.unreliable).toBe(true) // 缺出网段
    })

    it('四段全到达 → 完整权重,unreliable=false,partial=false', () => {
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
      const result = calculateExamScore(report)
      // 稳定性 85×0.4 + 速度 90×0.25 + 解锁 100×0.2 + 出网 100×0.15 = 91.5
      expect(result.total).toBeCloseTo(91.5, 1)
      expect(result.partial).toBe(false)
      expect(result.unreliable).toBe(false)
    })

    it('出网全失败在渐进模式也强制 0 分', () => {
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
      const result = calculateExamScore(report)
      expect(result.total).toBe(0)
      expect(result.unreliable).toBe(true)
      expect(result.partial).toBe(true)
    })

    it('渐进式评分数值单调递增(段逐项到达)', () => {
      // 第一帧:只有稳定性
      const report1: ExamReport = { stability: { score: 80 } as any }
      const score1 = calculateExamScore(report1).total

      // 第二帧:稳定性 + 速度
      const report2: ExamReport = {
        stability: { score: 80 } as any,
        region_speed: { regions: [{ name: '基准', code: 'baseline', down_mbps: 50, ttfb_ms: 10 }] }
      }
      const score2 = calculateExamScore(report2).total

      // 第三帧:稳定性 + 速度 + 解锁
      const report3: ExamReport = {
        ...report2,
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
      const score3 = calculateExamScore(report3).total

      // 第四帧:四段全到
      const report4: ExamReport = {
        ...report3,
        egress: {
          ipv4: { ip: '1.2.3.4', hosting: false },
          ipv6: { available: true },
          dns: { leak: false }
        } as any
      }
      const score4 = calculateExamScore(report4).total

      // 验证单调递增(或相等,因为出网可能扣分)
      expect(score2).toBeGreaterThan(score1)
      expect(score3).toBeGreaterThan(score2)
      expect(score4).toBeGreaterThanOrEqual(score3) // 出网满分时增长,扣分时可能减少
    })
  })
})
