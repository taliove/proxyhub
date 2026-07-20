import { describe, it, expect } from 'vitest'
import { hhmmToDate, dateToHhmm } from './scheduletime'

describe('dateToHhmm', () => {
  it('零填充时分', () => {
    expect(dateToHhmm(new Date('2000-01-01T03:05:00'))).toBe('03:05')
    expect(dateToHhmm(new Date('2000-01-01T23:59:00'))).toBe('23:59')
  })
  it('非法 Date 回落到 03:30', () => {
    expect(dateToHhmm(new Date('invalid'))).toBe('03:30')
  })
})

describe('hhmmToDate', () => {
  it('往返一致', () => {
    expect(dateToHhmm(hhmmToDate('02:15'))).toBe('02:15')
    expect(dateToHhmm(hhmmToDate('00:00'))).toBe('00:00')
  })
  it('非法输入回落到 03:30', () => {
    expect(dateToHhmm(hhmmToDate('3:30'))).toBe('03:30')
    expect(dateToHhmm(hhmmToDate('24:00'))).toBe('03:30')
    expect(dateToHhmm(hhmmToDate('12:60'))).toBe('03:30')
    expect(dateToHhmm(hhmmToDate(''))).toBe('03:30')
  })
})
