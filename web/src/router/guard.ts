// Navigation guard rules, kept out of index.ts so they can be tested without
// pulling in the lazy-loaded view graph (echarts, codemirror et al).
//
// The order below is not arbitrary: it mirrors the backend middleware nesting
// in internal/server/server.go. requirePasswordChanged wraps the MFA
// enrollment endpoint, so a user who owes both a password change and an
// enrollment must do the password first - sending them to the binding page
// would only dead-end on a 403.

// FORCED_PASSWORD_PATH / FORCED_MFA_PATH are the two pages that are exempt
// from their own redirect (otherwise the guard would loop).
export const FORCED_PASSWORD_PATH = '/change-password'
export const FORCED_MFA_PATH = '/mfa/enroll'

// GuardState is the slice of auth state the guard reads.
export interface GuardState {
  isAuthenticated: boolean
  mustChangePassword: boolean
  mustEnrollMFA: boolean
  isSuperAdmin: boolean
}

// GuardTarget is the route being entered, reduced to what the rules need.
export interface GuardTarget {
  path: string
  skipAuth?: boolean
  requiresSuperAdmin?: boolean
}

// resolveRedirect returns the path to redirect to, or null to let the
// navigation through.
export function resolveRedirect(to: GuardTarget, state: GuardState): string | null {
  if (to.skipAuth) return null
  if (!state.isAuthenticated) return '/login'
  if (state.mustChangePassword && to.path !== FORCED_PASSWORD_PATH) {
    // 首登强制改密(ticket 04):must_change_password 未清除前不许进业务页
    return FORCED_PASSWORD_PATH
  }
  if (state.mustEnrollMFA && to.path !== FORCED_MFA_PATH) {
    // 强制 MFA 绑定(ticket 08):改密之后、业务页之前的第二道闸
    return FORCED_MFA_PATH
  }
  if (to.requiresSuperAdmin && !state.isSuperAdmin) {
    // Non-super-admin users are bounced to home; admin APIs would 403 anyway
    return '/'
  }
  return null
}
