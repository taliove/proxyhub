// Guard rule tests for the forced flows: auth (ticket 02), password change
// (ticket 04), MFA enrollment (ticket 08), and the super-admin gate.
import { describe, it, expect } from 'vitest'
import { FORCED_MFA_PATH, FORCED_PASSWORD_PATH, resolveRedirect, type GuardState } from './guard'

const state = (over: Partial<GuardState> = {}): GuardState => ({
  isAuthenticated: true,
  mustChangePassword: false,
  mustEnrollMFA: false,
  isSuperAdmin: false,
  ...over
})

describe('resolveRedirect: must_enroll_mfa', () => {
  it('未绑定用户访问业务页被拦到绑定页', () => {
    expect(resolveRedirect({ path: '/nodes' }, state({ mustEnrollMFA: true }))).toBe(
      FORCED_MFA_PATH
    )
  })

  it('未绑定用户访问首页同样被拦', () => {
    expect(resolveRedirect({ path: '/' }, state({ mustEnrollMFA: true }))).toBe(FORCED_MFA_PATH)
  })

  it('绑定页自身豁免，不产生重定向循环', () => {
    expect(resolveRedirect({ path: FORCED_MFA_PATH }, state({ mustEnrollMFA: true }))).toBeNull()
  })

  it('清除强制位后放行', () => {
    expect(resolveRedirect({ path: '/nodes' }, state({ mustEnrollMFA: false }))).toBeNull()
  })

  it('超管也躲不过绑定（后端 adminGuard 同样 403）', () => {
    expect(
      resolveRedirect(
        { path: '/admin/users', requiresSuperAdmin: true },
        state({ mustEnrollMFA: true, isSuperAdmin: true })
      )
    ).toBe(FORCED_MFA_PATH)
  })
})

describe('resolveRedirect: 优先级', () => {
  it('改密优先于绑定（与后端中间件嵌套一致）', () => {
    expect(
      resolveRedirect({ path: '/nodes' }, state({ mustChangePassword: true, mustEnrollMFA: true }))
    ).toBe(FORCED_PASSWORD_PATH)
  })

  it('欠改密时连绑定页也不放，先回改密页', () => {
    expect(
      resolveRedirect(
        { path: FORCED_MFA_PATH },
        state({ mustChangePassword: true, mustEnrollMFA: true })
      )
    ).toBe(FORCED_PASSWORD_PATH)
  })

  it('未登录优先于一切强制位', () => {
    expect(
      resolveRedirect(
        { path: '/nodes' },
        state({ isAuthenticated: false, mustEnrollMFA: true, mustChangePassword: true })
      )
    ).toBe('/login')
  })

  it('skipAuth 页（登录/初始化）始终放行', () => {
    expect(
      resolveRedirect(
        { path: '/login', skipAuth: true },
        state({ isAuthenticated: false, mustEnrollMFA: true })
      )
    ).toBeNull()
  })
})

describe('resolveRedirect: 既有规则不回归', () => {
  it('未登录去业务页回登录页', () => {
    expect(resolveRedirect({ path: '/nodes' }, state({ isAuthenticated: false }))).toBe('/login')
  })

  it('欠改密时拦到改密页，改密页自身豁免', () => {
    expect(resolveRedirect({ path: '/nodes' }, state({ mustChangePassword: true }))).toBe(
      FORCED_PASSWORD_PATH
    )
    expect(
      resolveRedirect({ path: FORCED_PASSWORD_PATH }, state({ mustChangePassword: true }))
    ).toBeNull()
  })

  it('非超管访问超管页被送回首页', () => {
    expect(resolveRedirect({ path: '/audit', requiresSuperAdmin: true }, state())).toBe('/')
  })

  it('超管可进超管页', () => {
    expect(
      resolveRedirect({ path: '/audit', requiresSuperAdmin: true }, state({ isSuperAdmin: true }))
    ).toBeNull()
  })

  it('普通已登录用户正常进业务页', () => {
    expect(resolveRedirect({ path: '/endpoints' }, state())).toBeNull()
  })
})
