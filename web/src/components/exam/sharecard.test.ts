import { describe, it, expect } from 'vitest'
import type { ExamReport } from '@/types'
import {
  maskNodeName,
  displayNodeName,
  formatExamTime,
  shareScore,
  shareBaselineMbps,
  shareBaselineUpMbps,
  shareRegionExtremes,
  shareUnlockCells,
  shareEgressSummary,
  unlockLevelColorVar,
  leakColorVar,
  leakLabel,
  MASK,
  UNNAMED
} from './sharecard'

describe('maskNodeName', () => {
  it('保留前两段并遮蔽尾段(三段名匹配 ticket 示例)', () => {
    expect(maskNodeName('233boy-grpc-1.2.3.4')).toBe(`233boy-grpc-${MASK}`)
  })

  it('两段名只保留首段,遮蔽尾段(不泄露全部)', () => {
    expect(maskNodeName('233boy-hk')).toBe(`233boy-${MASK}`)
  })

  it('多段名(>3)至多保留前两段', () => {
    expect(maskNodeName('a-b-c-d')).toBe(`a-b-${MASK}`)
  })

  it('兼容空格/竖线/at 等分隔符', () => {
    expect(maskNodeName('香港 grpc host')).toBe(`香港-grpc-${MASK}`)
    expect(maskNodeName('us1|premium|host')).toBe(`us1-premium-${MASK}`)
  })

  it('不按点号切段:纯域名/IP 单段只露极短前缀', () => {
    expect(maskNodeName('hk1.example.com')).toBe(`hk${MASK}`)
    expect(maskNodeName('1.2.3.4')).toBe(`1.${MASK}`)
  })

  it('单段中文名保留前两字', () => {
    expect(maskNodeName('香港东京节点')).toBe(`香港${MASK}`)
  })

  it('空名/纯空白回落占位', () => {
    expect(maskNodeName('')).toBe(UNNAMED)
    expect(maskNodeName('   ')).toBe(UNNAMED)
  })
})

describe('displayNodeName', () => {
  it('打码开:走 maskNodeName', () => {
    expect(displayNodeName('233boy-grpc-host', true)).toBe(`233boy-grpc-${MASK}`)
  })
  it('打码关:原样(去空白)', () => {
    expect(displayNodeName('  233boy-grpc-host  ', false)).toBe('233boy-grpc-host')
  })
  it('打码关但空名:占位', () => {
    expect(displayNodeName('', false)).toBe(UNNAMED)
  })
})

describe('formatExamTime', () => {
  it('格式化为本地 YYYY-MM-DD HH:mm', () => {
    const d = new Date(2026, 6, 20, 9, 5)
    expect(formatExamTime(d)).toBe('2026-07-20 09:05')
  })
  it('接受时间戳数字', () => {
    const d = new Date(2026, 0, 1, 23, 59)
    expect(formatExamTime(d.getTime())).toBe('2026-01-01 23:59')
  })
  it('无效/缺失回落横杠', () => {
    expect(formatExamTime(undefined)).toBe('—')
    expect(formatExamTime('')).toBe('—')
    expect(formatExamTime('not-a-date')).toBe('—')
  })
})

describe('shareScore', () => {
  it('有稳定性段返回分数', () => {
    expect(shareScore({ stability: { score: 88 } } as ExamReport)).toBe(88)
  })
  it('无稳定性段返回 null', () => {
    expect(shareScore({} as ExamReport)).toBeNull()
  })
})

describe('shareBaselineMbps', () => {
  it('取基准行下行速率', () => {
    const report = {
      region_speed: {
        regions: [
          { code: 'x', name: '基准', ttfb_ms: 10, down_mbps: 500 },
          { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80 }
        ]
      }
    } as ExamReport
    expect(shareBaselineMbps(report)).toBe(500)
  })
  it('基准失败返回 null', () => {
    const report = {
      region_speed: {
        regions: [{ code: 'x', name: '基准', ttfb_ms: 0, down_mbps: 0, error: 'timeout' }]
      }
    } as ExamReport
    expect(shareBaselineMbps(report)).toBeNull()
  })
  it('无基准返回 null', () => {
    const report = {
      region_speed: { regions: [{ code: 'us_west', name: '美西', ttfb_ms: 1, down_mbps: 9 }] }
    } as ExamReport
    expect(shareBaselineMbps(report)).toBeNull()
  })
})

describe('shareBaselineUpMbps', () => {
  it('取基准行上行速率', () => {
    const report = {
      region_speed: {
        regions: [
          { code: 'x', name: '基准', ttfb_ms: 10, down_mbps: 500, up_mbps: 100 },
          { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80 }
        ]
      }
    } as ExamReport
    expect(shareBaselineUpMbps(report)).toBe(100)
  })
  it('基准行无上行字段返回 null(票据 33 未落地)', () => {
    const report = {
      region_speed: {
        regions: [{ code: 'x', name: '基准', ttfb_ms: 10, down_mbps: 500 }]
      }
    } as ExamReport
    expect(shareBaselineUpMbps(report)).toBeNull()
  })
  it('基准失败返回 null', () => {
    const report = {
      region_speed: {
        regions: [
          { code: 'x', name: '基准', ttfb_ms: 0, down_mbps: 0, up_mbps: 0, error: 'timeout' }
        ]
      }
    } as ExamReport
    expect(shareBaselineUpMbps(report)).toBeNull()
  })
  it('无基准返回 null', () => {
    const report = {
      region_speed: {
        regions: [{ code: 'us_west', name: '美西', ttfb_ms: 1, down_mbps: 9, up_mbps: 5 }]
      }
    } as ExamReport
    expect(shareBaselineUpMbps(report)).toBeNull()
  })
})

describe('shareRegionExtremes', () => {
  it('排除基准,取下行最高=最佳、最低=最差;包含 ttfb_ms', () => {
    const report = {
      region_speed: {
        regions: [
          { code: 'b', name: '基准', ttfb_ms: 5, down_mbps: 999 },
          { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80 },
          { code: 'jp', name: '东京', ttfb_ms: 60, down_mbps: 150 },
          { code: 'sg', name: '新加坡', ttfb_ms: 90, down_mbps: 40 },
          { code: 'err', name: '孟买', ttfb_ms: 0, down_mbps: 0, error: 'fail' }
        ]
      }
    } as ExamReport
    const { best, worst } = shareRegionExtremes(report)
    expect(best?.name).toBe('东京')
    expect(best?.down_mbps).toBe(150)
    expect(best?.ttfb_ms).toBe(60)
    expect(worst?.name).toBe('新加坡')
    expect(worst?.down_mbps).toBe(40)
    expect(worst?.ttfb_ms).toBe(90)
  })
  it('单个有效区域:最佳=最差=该区', () => {
    const report = {
      region_speed: { regions: [{ code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80 }] }
    } as ExamReport
    const { best, worst } = shareRegionExtremes(report)
    expect(best?.name).toBe('美西')
    expect(worst?.name).toBe('美西')
  })
  it('无有效区域:双 null', () => {
    expect(shareRegionExtremes({} as ExamReport)).toEqual({ best: null, worst: null })
  })
})

describe('shareUnlockCells', () => {
  it('固定 6 槽位归位,缺失为 unknown/未测', () => {
    const report = {
      unlock: {
        results: [
          { node_key: 'k', target_name: 'Netflix', available: true, latency: 1, level: 'full' },
          { node_key: 'k', target_name: 'OpenAI', available: false, latency: 1, level: 'blocked' }
        ]
      }
    } as ExamReport
    const cells = shareUnlockCells(report)
    expect(cells).toHaveLength(6)
    expect(cells[0]).toMatchObject({ name: 'Netflix', level: 'full' })
    const openai = cells.find((c) => c.name === 'OpenAI')
    expect(openai?.level).toBe('blocked')
    const gemini = cells.find((c) => c.name === 'Gemini')
    expect(gemini).toMatchObject({ level: 'unknown', label: '未测' })
  })
})

describe('shareEgressSummary', () => {
  it('取 IPv4 地区与 DNS 泄露状态;默认不返回 IP', () => {
    const report = {
      egress: {
        ipv4: {
          ip: '203.0.113.7',
          country: '美国',
          region: '加州',
          city: '洛杉矶',
          proxy: false,
          hosting: true
        },
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: true }
      }
    } as ExamReport
    const s = shareEgressSummary(report)
    expect(s.ipv4Region).toBe('美国 · 加州 · 洛杉矶')
    expect(s.dnsLeak).toBe('leak')
    expect(s.ipv4Address).toBeUndefined()
  })
  it('showIp=true 时返回 IPv4 地址', () => {
    const report = {
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
    } as ExamReport
    const s = shareEgressSummary(report, true)
    expect(s.ipv4Address).toBe('203.0.113.7')
    expect(s.ipv4Region).toBe('美国 · 加州 · 洛杉矶')
    expect(s.dnsLeak).toBe('ok')
  })
  it('DNS 无泄露 -> ok;探测异常 -> unknown', () => {
    expect(shareEgressSummary({ egress: { dns: { leak: false } } } as ExamReport).dnsLeak).toBe(
      'ok'
    )
    expect(
      shareEgressSummary({ egress: { dns: { leak: false, error: 'x' } } } as ExamReport).dnsLeak
    ).toBe('unknown')
  })
  it('IPv4 探测失败 -> 地区为空,无 IP', () => {
    const report = { egress: { ipv4: { proxy: false, hosting: false, error: 'x' } } } as ExamReport
    const s = shareEgressSummary(report, true)
    expect(s.ipv4Region).toBe('')
    expect(s.ipv4Address).toBeUndefined()
  })

  it('安全:默认态摘要不含出口 IP / 解析器 IP 等地址字段', () => {
    const report = {
      egress: {
        ipv4: {
          ip: '203.0.113.7',
          country: '美国',
          region: '加州',
          city: '洛杉矶',
          asn: 'AS64500',
          org: 'Example Hosting',
          proxy: true,
          hosting: true
        },
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
      }
    } as ExamReport
    const serialized = JSON.stringify(shareEgressSummary(report, false))
    expect(serialized).not.toContain('203.0.113.7')
    expect(serialized).not.toContain('8.8.8.8')
    expect(serialized).not.toContain('AS64500')
  })
  it('安全:showIp=true 属用户明示,可序列化出 IP', () => {
    const report = {
      egress: {
        ipv4: {
          ip: '203.0.113.7',
          country: '美国',
          proxy: false,
          hosting: true
        }
      }
    } as ExamReport
    const serialized = JSON.stringify(shareEgressSummary(report, true))
    expect(serialized).toContain('203.0.113.7')
  })
})

describe('color/label mappings', () => {
  it('unlockLevelColorVar 三档 + 中性', () => {
    expect(unlockLevelColorVar('full')).toBe('--ph-success')
    expect(unlockLevelColorVar('originals_only')).toBe('--ph-warning')
    expect(unlockLevelColorVar('blocked')).toBe('--ph-danger')
    expect(unlockLevelColorVar('unknown')).toBe('--ph-text-secondary')
  })
  it('leak 状态色与标签', () => {
    expect(leakColorVar('ok')).toBe('--ph-success')
    expect(leakColorVar('leak')).toBe('--ph-danger')
    expect(leakColorVar('unknown')).toBe('--ph-text-secondary')
    expect(leakLabel('ok')).toBe('未泄露')
    expect(leakLabel('leak')).toBe('疑似泄露')
    expect(leakLabel('unknown')).toBe('未知')
  })
})
