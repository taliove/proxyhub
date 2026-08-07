// appBase 测试:注入与缺省两种形态
import { describe, it, expect, afterEach } from 'vitest'
import { appBase } from '@/utils/base'

describe('appBase', () => {
  afterEach(() => {
    delete window.__PH_BASE__
  })

  it('未注入时返回空串（开发/根路径部署）', () => {
    expect(appBase()).toBe('')
  })

  it('注入 Site Path 时原样返回', () => {
    window.__PH_BASE__ = '/GTsRiXWBKs7El92a1HJ9'
    expect(appBase()).toBe('/GTsRiXWBKs7El92a1HJ9')
  })
})
