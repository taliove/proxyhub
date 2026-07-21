import { describe, it, expect } from 'vitest'
import { scoreColor, testTimeRelative, scoreDisplay } from './airport-test-utils'

describe('scoreColor', () => {
  it('returns success for scores >= 80', () => {
    expect(scoreColor(80)).toBe('success')
    expect(scoreColor(85.5)).toBe('success')
    expect(scoreColor(100)).toBe('success')
  })

  it('returns warning for scores >= 60 and < 80', () => {
    expect(scoreColor(60)).toBe('warning')
    expect(scoreColor(70)).toBe('warning')
    expect(scoreColor(79.9)).toBe('warning')
  })

  it('returns danger for scores < 60', () => {
    expect(scoreColor(0)).toBe('danger')
    expect(scoreColor(30)).toBe('danger')
    expect(scoreColor(59.9)).toBe('danger')
  })

  it('returns info for null/undefined score', () => {
    expect(scoreColor(null)).toBe('info')
    expect(scoreColor(undefined)).toBe('info')
  })

  it('returns info for failed status with null score', () => {
    expect(scoreColor(null, 'failed')).toBe('info')
    expect(scoreColor(undefined, 'failed')).toBe('info')
  })
})

describe('testTimeRelative', () => {
  it('returns "-" for null/undefined', () => {
    expect(testTimeRelative(null)).toBe('-')
    expect(testTimeRelative(undefined)).toBe('-')
  })

  it('formats recent time correctly', () => {
    const now = Date.now()
    const threeMinAgo = new Date(now - 3 * 60 * 1000).toISOString()
    expect(testTimeRelative(threeMinAgo)).toBe('3分钟前')
  })

  it('formats hours correctly', () => {
    const now = Date.now()
    const twoHoursAgo = new Date(now - 2 * 60 * 60 * 1000).toISOString()
    expect(testTimeRelative(twoHoursAgo)).toBe('2小时前')
  })

  it('formats days correctly', () => {
    const now = Date.now()
    const threeDaysAgo = new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString()
    expect(testTimeRelative(threeDaysAgo)).toBe('3天前')
  })

  it('formats old dates as MM-DD', () => {
    const now = Date.now()
    const tenDaysAgo = new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString()
    const result = testTimeRelative(tenDaysAgo)
    expect(result).toMatch(/^\d{2}-\d{2}$/)
  })

  it('handles "just now" for very recent times', () => {
    const now = Date.now()
    const tenSecsAgo = new Date(now - 10 * 1000).toISOString()
    expect(testTimeRelative(tenSecsAgo)).toBe('刚刚')
  })
})

describe('scoreDisplay', () => {
  it('formats score with one decimal place', () => {
    expect(scoreDisplay(85.5)).toBe('85.5')
    expect(scoreDisplay(90)).toBe('90.0')
    expect(scoreDisplay(67.89)).toBe('67.9')
  })

  it('returns "-" for null/undefined', () => {
    expect(scoreDisplay(null)).toBe('-')
    expect(scoreDisplay(undefined)).toBe('-')
  })
})
