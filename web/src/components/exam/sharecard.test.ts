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
  shareViewModel,
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
  it('默认不返回任何 IP/服务器地址,仅返回地区与泄露状态', () => {
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
    const s = shareEgressSummary(report, { ingressIp: '1.2.3.4' })
    expect(s.ipv4Region).toBe('美国 · 加州 · 洛杉矶')
    expect(s.dnsLeak).toBe('leak')
    expect(s.egressIp).toBeUndefined()
    expect(s.ingressIp).toBeUndefined()
    expect(s.dnsResolver).toBeUndefined()
  })

  it('showEgressIp=true 时返回出口 IPv4 地址', () => {
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
    const s = shareEgressSummary(report, { showEgressIp: true })
    expect(s.egressIp).toBe('203.0.113.7')
    expect(s.ipv4Region).toBe('美国 · 加州 · 洛杉矶')
    expect(s.dnsLeak).toBe('ok')
    expect(s.ingressIp).toBeUndefined()
    expect(s.dnsResolver).toBeUndefined()
  })

  it('showIngressIp=true 时返回入口 IP(节点服务器地址)', () => {
    const report = { egress: {} } as ExamReport
    const s = shareEgressSummary(report, { showIngressIp: true, ingressIp: '1.2.3.4' })
    expect(s.ingressIp).toBe('1.2.3.4')
    expect(s.egressIp).toBeUndefined()
    expect(s.dnsResolver).toBeUndefined()
  })

  it('showDns=true 时返回 DNS 解析器 IP + 地区', () => {
    const report = {
      egress: {
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
      }
    } as ExamReport
    const s = shareEgressSummary(report, { showDns: true })
    expect(s.dnsResolver).toBe('8.8.8.8 (美国)')
    expect(s.dnsLeak).toBe('ok')
    expect(s.egressIp).toBeUndefined()
    expect(s.ingressIp).toBeUndefined()
  })

  it('showDns=true 且解析器无地区:仅显示 IP', () => {
    const report = {
      egress: {
        dns: { resolver_ip: '8.8.8.8', leak: false }
      }
    } as ExamReport
    const s = shareEgressSummary(report, { showDns: true })
    expect(s.dnsResolver).toBe('8.8.8.8')
  })

  it('三个开关全开:同时返回三个地址字段', () => {
    const report = {
      egress: {
        ipv4: { ip: '203.0.113.7', country: '美国', proxy: false, hosting: true },
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
      }
    } as ExamReport
    const s = shareEgressSummary(report, {
      showEgressIp: true,
      showIngressIp: true,
      showDns: true,
      ingressIp: '1.2.3.4'
    })
    expect(s.egressIp).toBe('203.0.113.7')
    expect(s.ingressIp).toBe('1.2.3.4')
    expect(s.dnsResolver).toBe('8.8.8.8 (美国)')
  })

  it('DNS 泄露状态与 showDns 独立:状态始终返回,解析器详情受开关控制', () => {
    const report = {
      egress: {
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: true }
      }
    } as ExamReport
    const s1 = shareEgressSummary(report, { showDns: false })
    expect(s1.dnsLeak).toBe('leak')
    expect(s1.dnsResolver).toBeUndefined()
    const s2 = shareEgressSummary(report, { showDns: true })
    expect(s2.dnsLeak).toBe('leak')
    expect(s2.dnsResolver).toBe('8.8.8.8 (美国)')
  })

  it('DNS 无泄露 -> ok;探测异常 -> unknown', () => {
    expect(shareEgressSummary({ egress: { dns: { leak: false } } } as ExamReport).dnsLeak).toBe(
      'ok'
    )
    expect(
      shareEgressSummary({ egress: { dns: { leak: false, error: 'x' } } } as ExamReport).dnsLeak
    ).toBe('unknown')
  })

  it('IPv4 探测失败 -> 地区为空,无出口 IP', () => {
    const report = { egress: { ipv4: { proxy: false, hosting: false, error: 'x' } } } as ExamReport
    const s = shareEgressSummary(report, { showEgressIp: true })
    expect(s.ipv4Region).toBe('')
    expect(s.egressIp).toBeUndefined()
  })

  it('安全:默认态摘要不含任何 IP/服务器地址', () => {
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
    const serialized = JSON.stringify(shareEgressSummary(report, { ingressIp: '1.2.3.4' }))
    expect(serialized).not.toContain('203.0.113.7')
    expect(serialized).not.toContain('8.8.8.8')
    expect(serialized).not.toContain('1.2.3.4')
    expect(serialized).not.toContain('AS64500')
  })

  it('安全:各开关打开属用户明示,对应字段可序列化', () => {
    const report = {
      egress: {
        ipv4: { ip: '203.0.113.7', country: '美国', proxy: false, hosting: true },
        dns: { resolver_ip: '8.8.8.8', resolver_geo: '美国', leak: false }
      }
    } as ExamReport
    const s1 = shareEgressSummary(report, { showEgressIp: true })
    expect(JSON.stringify(s1)).toContain('203.0.113.7')
    expect(JSON.stringify(s1)).not.toContain('8.8.8.8')
    expect(JSON.stringify(s1)).not.toContain('1.2.3.4')

    const s2 = shareEgressSummary(report, { showIngressIp: true, ingressIp: '1.2.3.4' })
    expect(JSON.stringify(s2)).toContain('1.2.3.4')
    expect(JSON.stringify(s2)).not.toContain('203.0.113.7')

    const s3 = shareEgressSummary(report, { showDns: true })
    expect(JSON.stringify(s3)).toContain('8.8.8.8')
    expect(JSON.stringify(s3)).not.toContain('203.0.113.7')
  })
})

describe('shareViewModel (统一视图模型:showAll 控制全量/摘要)', () => {
  const fullReport: ExamReport = {
    stability: {
      score: 88,
      total: 100,
      succeeded: 95,
      loss_rate: 0.05,
      mean_ms: 45,
      median_ms: 42,
      p95_ms: 78,
      p99_ms: 95,
      jitter_ms: 8
    },
    region_speed: {
      regions: [
        { code: 'x', name: '基准', ttfb_ms: 10, down_mbps: 500, up_mbps: 100 },
        { code: 'us_west', name: '美西', ttfb_ms: 120, down_mbps: 80, up_mbps: 20 },
        { code: 'jp', name: '东京', ttfb_ms: 60, down_mbps: 150, up_mbps: 30 },
        { code: 'sg', name: '新加坡', ttfb_ms: 90, down_mbps: 40, up_mbps: 10 }
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

  it('showAll=false(默认):摘要版 - 打码节点名、无任何 IP、多地域仅最佳/最差', () => {
    const vm = shareViewModel(fullReport, {
      showAll: false,
      nodeName: '233boy-grpc-host',
      ingressIp: '1.2.3.4',
      examTime: new Date(2026, 6, 20, 9, 5)
    })
    // 节点名打码
    expect(vm.nodeLabel).toBe('233boy-grpc-***')
    // 摘要仅最佳/最差
    expect(vm.regionSummary).toEqual({ best: expect.any(Object), worst: expect.any(Object) })
    expect(vm.regionSummary.best?.name).toBe('东京')
    expect(vm.regionSummary.worst?.name).toBe('新加坡')
    // 无全量区域列表
    expect(vm.allRegions).toBeUndefined()
    // 无稳定性明细
    expect(vm.stabilityDetails).toBeUndefined()
    // 出口无任何 IP
    expect(vm.egress.egressIp).toBeUndefined()
    expect(vm.egress.ingressIp).toBeUndefined()
    expect(vm.egress.dnsResolver).toBeUndefined()
    expect(vm.egress.ipv4Region).toBe('美国 · 加州 · 洛杉矶')
    // 安全断言:序列化不含任何敏感字段
    const serialized = JSON.stringify(vm)
    expect(serialized).not.toContain('203.0.113.7')
    expect(serialized).not.toContain('8.8.8.8')
    expect(serialized).not.toContain('1.2.3.4')
    expect(serialized).not.toContain('AS64500')
  })

  it('showAll=true:全量版 - 完整节点名、所有 IP、多地域全行、稳定性明细、出网全字段', () => {
    const vm = shareViewModel(fullReport, {
      showAll: true,
      nodeName: '233boy-grpc-host',
      ingressIp: '1.2.3.4',
      examTime: new Date(2026, 6, 20, 9, 5)
    })
    // 完整节点名
    expect(vm.nodeLabel).toBe('233boy-grpc-host')
    // 多地域全行(除基准外 3 行)
    expect(vm.allRegions).toHaveLength(3)
    expect(vm.allRegions?.map((r) => r.name)).toEqual(['美西', '东京', '新加坡'])
    expect(vm.allRegions?.[0].up_mbps).toBe(20)
    // 仍有摘要(向后兼容)
    expect(vm.regionSummary.best?.name).toBe('东京')
    // 稳定性明细
    expect(vm.stabilityDetails).toEqual({
      score: 88,
      total: 100,
      succeeded: 95,
      loss_rate: 0.05,
      mean_ms: 45,
      median_ms: 42,
      p95_ms: 78,
      p99_ms: 95,
      jitter_ms: 8
    })
    // 出口全字段
    expect(vm.egress.egressIp).toBe('203.0.113.7')
    expect(vm.egress.ingressIp).toBe('1.2.3.4')
    expect(vm.egress.dnsResolver).toBe('8.8.8.8 (美国)')
    expect(vm.egress.asn).toBe('AS64500')
    expect(vm.egress.org).toBe('Example Hosting')
    expect(vm.egress.proxy).toBe(false)
    expect(vm.egress.hosting).toBe(true)
  })

  it('showAll=true 但稳定性段缺失:stabilityDetails 为 undefined', () => {
    const report = { ...fullReport, stability: undefined }
    const vm = shareViewModel(report, { showAll: true, nodeName: 'test' })
    expect(vm.stabilityDetails).toBeUndefined()
  })

  it('showAll=true 但多地域段缺失:allRegions 为空数组', () => {
    const report = { ...fullReport, region_speed: undefined }
    const vm = shareViewModel(report, { showAll: true, nodeName: 'test' })
    expect(vm.allRegions).toEqual([])
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
