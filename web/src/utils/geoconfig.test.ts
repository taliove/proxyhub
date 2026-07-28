// 地域白名单配置口径的单测(pull-guard ticket 08):geo_mode 三档映射与
// 数组<->逗号字符串转换。这些映射的键必须覆盖后端 store.GeoMode* 常量。
import { describe, it, expect } from 'vitest'
import {
  COUNTRY_OPTIONS,
  geoModeDesc,
  geoModeLabel,
  geoModeTag,
  joinGeoList,
  parseGeoList
} from './geoconfig'

describe('geoModeLabel', () => {
  // 键即 internal/store/endpoint_geo.go 的三档常量
  it.each([
    ['off', '关闭'],
    ['observe', '观察'],
    ['enforce', '拦截']
  ])('maps %s to %s', (mode, label) => {
    expect(geoModeLabel(mode)).toBe(label)
  })

  it('treats empty mode as off (backend default)', () => {
    expect(geoModeLabel('')).toBe('关闭')
  })

  it('passes unknown modes through (forward compat)', () => {
    expect(geoModeLabel('audit')).toBe('audit')
  })
})

describe('geoModeTag', () => {
  it('uses neutral for off, warning for observe, danger for enforce', () => {
    expect(geoModeTag('off')).toBe('info')
    expect(geoModeTag('observe')).toBe('warning')
    expect(geoModeTag('enforce')).toBe('danger')
  })

  it('treats empty mode as off', () => {
    expect(geoModeTag('')).toBe('info')
  })
})

describe('geoModeDesc', () => {
  it('provides description text for each mode', () => {
    expect(geoModeDesc('off')).toContain('不判定地域')
    expect(geoModeDesc('observe')).toContain('判定但不拦截')
    expect(geoModeDesc('enforce')).toContain('不匹配白名单则拒绝')
  })
})

describe('parseGeoList', () => {
  it('splits a comma-separated string into an array', () => {
    expect(parseGeoList('CN,JP,US')).toEqual(['CN', 'JP', 'US'])
  })

  it('trims whitespace around entries', () => {
    expect(parseGeoList(' CN , JP , US ')).toEqual(['CN', 'JP', 'US'])
  })

  it('drops empty entries', () => {
    expect(parseGeoList('CN,,JP')).toEqual(['CN', 'JP'])
    expect(parseGeoList('CN, , JP')).toEqual(['CN', 'JP'])
  })

  it('returns an empty array for an empty string', () => {
    expect(parseGeoList('')).toEqual([])
  })
})

describe('joinGeoList', () => {
  it('joins an array into a comma-separated string', () => {
    expect(joinGeoList(['CN', 'JP', 'US'])).toBe('CN,JP,US')
  })

  it('filters out empty strings', () => {
    expect(joinGeoList(['CN', '', 'JP'])).toBe('CN,JP')
    expect(joinGeoList(['CN', '  ', 'JP'])).toBe('CN,JP')
  })

  it('returns an empty string for an empty array', () => {
    expect(joinGeoList([])).toBe('')
  })
})

describe('COUNTRY_OPTIONS', () => {
  it('includes common countries with ISO 3166-1 alpha-2 codes', () => {
    const codes = COUNTRY_OPTIONS.map((c) => c.code)
    expect(codes).toContain('CN')
    expect(codes).toContain('HK')
    expect(codes).toContain('US')
    expect(codes).toContain('JP')
  })

  it('all codes are uppercase two-letter strings', () => {
    for (const { code } of COUNTRY_OPTIONS) {
      expect(code).toMatch(/^[A-Z]{2}$/)
    }
  })

  it('all names are non-empty strings', () => {
    for (const { name } of COUNTRY_OPTIONS) {
      expect(name.length).toBeGreaterThan(0)
    }
  })
})
