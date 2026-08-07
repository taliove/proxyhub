import { describe, it, expect } from 'vitest'
import {
  buildRegionRows,
  buildUnlockRows,
  buildEgressRows,
  regionSectionComplete,
  egressSectionComplete,
  isBaselineRow,
  BASELINE_KEY,
  BASELINE_NAME,
  EXAM_REGION_SLOTS,
  EXAM_UNLOCK_SLOTS,
  EXAM_EGRESS_SLOTS
} from './examrows'
import type { ExamRegionResult, ExamUnlockResult, ExamEgressMetrics } from '@/types'

const region = (code: string, over: Partial<ExamRegionResult> = {}): ExamRegionResult => ({
  code,
  name: code.toUpperCase(),
  ttfb_ms: 40,
  down_mbps: 20,
  ...over
})

const baseline = (over: Partial<ExamRegionResult> = {}): ExamRegionResult =>
  region('cf_pop', { name: BASELINE_NAME, ...over })

const unlock = (target: string, over: Partial<ExamUnlockResult> = {}): ExamUnlockResult => ({
  node_key: 'n1',
  target_name: target,
  available: true,
  latency: 100,
  level: 'full',
  ...over
})

describe('buildRegionRows placeholder rendering', () => {
  it('renders 9 rows from an empty stream: baseline on top + 8 fixed regions, all waiting', () => {
    const rows = buildRegionRows([], false)
    expect(rows).toHaveLength(1 + EXAM_REGION_SLOTS.length)
    expect(rows[0].baseline).toBe(true)
    expect(rows[0].name).toBe(BASELINE_NAME)
    expect(rows[0].key).toBe(BASELINE_KEY)
    // 其余 8 行非基准,名称/顺序与固定槽位一致
    expect(rows.slice(1).every((r) => !r.baseline)).toBe(true)
    expect(rows.slice(1).map((r) => r.name)).toEqual(EXAM_REGION_SLOTS.map((s) => s.name))
    // 未到达全部 waiting、无数据
    expect(rows.every((r) => r.status === 'waiting')).toBe(true)
    expect(rows.every((r) => r.result === null)).toBe(true)
  })
})

describe('buildRegionRows active highlight transfer', () => {
  it('marks the baseline row active first when the section is running', () => {
    const rows = buildRegionRows([], true)
    expect(rows[0].status).toBe('active')
    expect(rows.slice(1).every((r) => r.status === 'waiting')).toBe(true)
  })

  it('transfers active to the next unfilled row as data arrives', () => {
    // 基准到达后,active 移交给第一区(美西)
    const afterBaseline = buildRegionRows([baseline()], true)
    expect(afterBaseline[0].status).toBe('ok')
    expect(afterBaseline[1].status).toBe('active')
    expect(afterBaseline[1].key).toBe('us_west')

    // 第一区到达后,active 移交给第二区(美东)
    const afterFirst = buildRegionRows([baseline(), region('us_west')], true)
    expect(afterFirst[1].status).toBe('ok')
    expect(afterFirst[2].status).toBe('active')
    expect(afterFirst[2].key).toBe('us_east')
  })

  it('does not assume arrival order: out-of-order rows land in their fixed slots', () => {
    // 东京先于美西到达:各自归位,active 仍是首个 waiting(基准)
    const rows = buildRegionRows([region('jp_tokyo')], true)
    const tokyo = rows.find((r) => r.key === 'jp_tokyo')!
    expect(tokyo.status).toBe('ok')
    expect(rows[0].status).toBe('active') // 基准尚未到达，仍为首个待处理
    // 位置固定:东京始终在第 5 个固定槽位
    expect(rows[5].key).toBe('jp_tokyo')
  })

  it('leaves no active row once every slot is settled', () => {
    const arrived = [baseline(), ...EXAM_REGION_SLOTS.map((s) => region(s.code))]
    const rows = buildRegionRows(arrived, true)
    expect(rows.every((r) => r.status === 'ok' || r.status === 'error')).toBe(true)
  })
})

describe('buildRegionRows baseline distinction', () => {
  it('routes a name=基准 row into the baseline slot regardless of its code', () => {
    const rows = buildRegionRows([baseline({ code: 'anything' })], false)
    expect(rows[0].baseline).toBe(true)
    expect(rows[0].result).not.toBeNull()
    expect(rows[0].status).toBe('ok')
    // 不新增第 10 行
    expect(rows).toHaveLength(1 + EXAM_REGION_SLOTS.length)
  })

  it('isBaselineRow keys purely on the name', () => {
    expect(isBaselineRow(baseline())).toBe(true)
    expect(isBaselineRow(region('us_west'))).toBe(false)
  })

  it('surfaces error status on a failed row', () => {
    const rows = buildRegionRows([region('us_west', { error: 'timeout' })], false)
    const west = rows.find((r) => r.key === 'us_west')!
    expect(west.status).toBe('error')
  })
})

describe('regionSectionComplete', () => {
  it('is false until all 8 fixed regions arrive; baseline is irrelevant', () => {
    expect(regionSectionComplete([])).toBe(false)
    expect(regionSectionComplete([baseline()])).toBe(false)
    const sevenOfEight = EXAM_REGION_SLOTS.slice(0, 7).map((s) => region(s.code))
    expect(regionSectionComplete(sevenOfEight)).toBe(false)
  })

  it('is true once all 8 fixed regions arrive, even without the baseline', () => {
    const all = EXAM_REGION_SLOTS.map((s) => region(s.code))
    expect(regionSectionComplete(all)).toBe(true)
  })
})

describe('buildUnlockRows', () => {
  it('renders 6 placeholder rows in the fixed order, all waiting', () => {
    const rows = buildUnlockRows([], false)
    expect(rows.map((r) => r.name)).toEqual([...EXAM_UNLOCK_SLOTS])
    expect(rows.every((r) => r.status === 'waiting' && r.result === null)).toBe(true)
  })

  it('fills arrived targets by name and transfers active to the next waiting', () => {
    const rows = buildUnlockRows([unlock('Netflix', { level: 'blocked' })], true)
    expect(rows[0].status).toBe('ok')
    expect(rows[0].result?.level).toBe('blocked')
    expect(rows[1].status).toBe('active')
    expect(rows[1].name).toBe('YouTube Premium')
  })

  it('keeps no row active when the section is not running (terminal)', () => {
    const rows = buildUnlockRows([unlock('Netflix')], false)
    expect(rows.some((r) => r.status === 'active')).toBe(false)
  })
})

describe('buildEgressRows', () => {
  const egress = (over: Partial<ExamEgressMetrics> = {}): ExamEgressMetrics => ({ ...over })

  it('renders 3 placeholder rows, all waiting on empty input', () => {
    const rows = buildEgressRows(null, false)
    expect(rows.map((r) => r.kind)).toEqual(EXAM_EGRESS_SLOTS.map((s) => s.kind))
    expect(rows.every((r) => r.status === 'waiting')).toBe(true)
  })

  it('marks arrived items ok/error and transfers active to the next waiting', () => {
    const rows = buildEgressRows(
      egress({ ipv4: { proxy: false, hosting: false, ip: '1.1.1.1' } }),
      true
    )
    expect(rows[0].status).toBe('ok')
    expect(rows[1].status).toBe('active') // ipv6 next
    expect(rows[2].status).toBe('waiting')
  })

  it('flags a failed item as error', () => {
    const rows = buildEgressRows(egress({ dns: { leak: false, error: 'boom' } }), false)
    const dns = rows.find((r) => r.kind === 'dns')!
    expect(dns.status).toBe('error')
  })

  it('keeps no row active in terminal state', () => {
    const rows = buildEgressRows(egress({ ipv4: { proxy: false, hosting: false } }), false)
    expect(rows.some((r) => r.status === 'active')).toBe(false)
  })
})

describe('egressSectionComplete', () => {
  const egress = (over: Partial<ExamEgressMetrics> = {}): ExamEgressMetrics => ({ ...over })

  it('is false with no egress data (section 1, still probing)', () => {
    expect(egressSectionComplete(null)).toBe(false)
    expect(egressSectionComplete(egress())).toBe(false)
  })

  it('is false until all 3 classes (ipv4/ipv6/dns) have arrived', () => {
    expect(egressSectionComplete(egress({ ipv4: { proxy: false, hosting: false } }))).toBe(false)
    expect(
      egressSectionComplete(
        egress({ ipv4: { proxy: false, hosting: false }, ipv6: { available: false } })
      )
    ).toBe(false)
  })

  it('is true once all 3 classes have arrived (error items still count as settled)', () => {
    const full = egress({
      ipv4: { proxy: false, hosting: false, ip: '203.0.113.7' },
      ipv6: { available: false },
      dns: { leak: false, error: 'boom' }
    })
    expect(egressSectionComplete(full)).toBe(true)
  })
})
