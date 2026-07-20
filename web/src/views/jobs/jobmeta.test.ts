import { describe, it, expect } from 'vitest'
import { kindLabel, statusMeta, isRunning, parseProgress, parseCursor } from './jobmeta'

describe('kindLabel', () => {
  it('已知 kind 映射中文名', () => {
    expect(kindLabel('exam')).toBe('单节点体检')
    expect(kindLabel('batch_exam')).toBe('批量体检')
    expect(kindLabel('batch_detection')).toBe('批量解锁检测')
    expect(kindLabel('retag_all')).toBe('晚间标签重算')
  })
  it('未知 kind 原样返回', () => {
    expect(kindLabel('mystery_job')).toBe('mystery_job')
  })
})

describe('statusMeta', () => {
  it('五态各有中文标签与语义色', () => {
    expect(statusMeta('running')).toEqual({ label: '运行中', tag: 'primary', running: true })
    expect(statusMeta('done')).toEqual({ label: '已完成', tag: 'success', running: false })
    expect(statusMeta('failed')).toEqual({ label: '失败', tag: 'danger', running: false })
    expect(statusMeta('cancelled')).toEqual({ label: '已取消', tag: 'info', running: false })
    expect(statusMeta('interrupted')).toEqual({ label: '已中断', tag: 'warning', running: false })
  })
  it('未知状态回落到 info + 原始值', () => {
    expect(statusMeta('weird')).toEqual({ label: 'weird', tag: 'info', running: false })
  })
})

describe('isRunning', () => {
  it('仅 running 为真', () => {
    expect(isRunning('running')).toBe(true)
    expect(isRunning('done')).toBe(false)
    expect(isRunning('interrupted')).toBe(false)
    expect(isRunning('unknown')).toBe(false)
  })
})

describe('parseCursor', () => {
  it('合法非负整数', () => {
    expect(parseCursor('0')).toBe(0)
    expect(parseCursor('42')).toBe(42)
  })
  it('空/非法/负数返回 null', () => {
    expect(parseCursor(undefined)).toBeNull()
    expect(parseCursor('')).toBeNull()
    expect(parseCursor('abc')).toBeNull()
    expect(parseCursor('-3')).toBeNull()
    expect(parseCursor('3.5')).toBeNull()
  })
})

describe('parseProgress', () => {
  it('无游标显示占位符', () => {
    expect(parseProgress(undefined)).toBe('-')
    expect(parseProgress('')).toBe('-')
    expect(parseProgress('bad')).toBe('-')
  })
  it('仅游标显示已处理计数', () => {
    expect(parseProgress('7')).toBe('已处理 7')
    expect(parseProgress('0')).toBe('已处理 0')
  })
  it('已知总量拼成 x/N', () => {
    expect(parseProgress('7', 20)).toBe('7/20')
  })
  it('总量为 0 或缺省时回落到计数', () => {
    expect(parseProgress('7', 0)).toBe('已处理 7')
  })
})
