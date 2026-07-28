// Pure helpers for the login page's second stage (ticket 09). Kept out of the
// .vue so the input normalization and the wire-error wording are unit-testable
// without mounting Element Plus - same split as views/login-utils.ts and
// views/mfa-enroll-utils.ts.

// LoginMFAMode is which kind of second factor the user is typing. One request
// field either way (POST /api/login/mfa {code}); the mode only drives the
// input affordances (placeholder, maxlength, normalization).
export type LoginMFAMode = 'totp' | 'recovery'

// TOTP_CODE_LENGTH mirrors the backend's 6-digit TOTP (handlers_mfa.go stages
// digits: 6).
export const TOTP_CODE_LENGTH = 6

// RECOVERY_CODE_LENGTH is the canonical recovery code length including the two
// dashes: XXXX-XXXX-XXXX (internal/mfa/recovery.go, 3 groups of 4).
export const RECOVERY_CODE_LENGTH = 14

// RECOVERY_CHARSET mirrors internal/mfa recoveryCharset: the visually
// ambiguous glyphs (0/O, 1/I/L) are absent, so anything outside this set is
// noise from a paste and can be dropped rather than sent.
const RECOVERY_CHARSET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789'

// normalizeTOTPCode strips non-digits and truncates: authenticators render
// codes as "123 456" and pasting that must not fail for a formatting reason.
export function normalizeTOTPCode(raw: string): string {
  return (raw ?? '').replace(/\D/g, '').slice(0, TOTP_CODE_LENGTH)
}

// normalizeRecoveryCode rebuilds the canonical XXXX-XXXX-XXXX form from
// whatever was typed (lowercase, missing or extra dashes, stray whitespace).
// This mirrors internal/mfa.normalizeRecoveryCode so the wire value matches
// what the server hashed; the server normalizes too, so this is only about
// showing the user a sane value while they type.
export function normalizeRecoveryCode(raw: string): string {
  const compact = Array.from((raw ?? '').toUpperCase())
    .filter((ch) => RECOVERY_CHARSET.includes(ch))
    .join('')
    .slice(0, 12)
  return (compact.match(/.{1,4}/g) ?? []).join('-')
}

// normalizeLoginMFACode dispatches on the active mode. Single write path: the
// views bind :model-value + @update:model-value through this, never v-model,
// so the displayed value and the submitted value can never diverge.
export function normalizeLoginMFACode(raw: string, mode: LoginMFAMode): string {
  return mode === 'totp' ? normalizeTOTPCode(raw) : normalizeRecoveryCode(raw)
}

// isCompleteLoginMFACode reports whether the code is worth sending. A short
// code would burn one of the 5 allowed attempts on the pending handoff (and
// one per-IP login failure) for nothing, so the submit button gates on it.
export function isCompleteLoginMFACode(raw: string, mode: LoginMFAMode): boolean {
  const value = normalizeLoginMFACode(raw, mode)
  return mode === 'totp' ? value.length === TOTP_CODE_LENGTH : value.length === RECOVERY_CODE_LENGTH
}

// errorShape digs the status and message out of an axios error. handleLoginMFA
// answers with http.Error (plain text) on every 401/403 path and with JSON
// {error} on the 400 "code is required" path, so both shapes are handled.
function errorShape(err: unknown): { status?: number; text: string } {
  const res = (err as { response?: { status?: number; data?: unknown } } | null)?.response
  const data = res?.data
  if (typeof data === 'string') return { status: res?.status, text: data.trim() }
  const body = (data ?? {}) as { error?: string; message?: string }
  return { status: res?.status, text: (body.error ?? body.message ?? '').trim() }
}

// loginMFAErrorMessage maps a failed second stage onto an actionable message.
//
// Every 401 is deliberately the same on the wire (wrong code, expired token,
// different source IP, attempt budget exhausted): telling them apart would map
// out the state for an attacker holding a stolen handoff. So the copy has to
// cover both readings at once - "re-enter, or go back and sign in again" -
// rather than guessing which one happened. The view pairs this with a visible
// "back to password" affordance for the unrecoverable half.
export function loginMFAErrorMessage(err: unknown): string {
  const { status, text } = errorShape(err)
  if (status === undefined) return '网络异常,请检查连接后重试'
  if (status === 401) return '验证码错误或已过期,请重新输入;若仍失败请返回重新登录'
  if (status === 400) return '请输入验证码'
  if (status === 403) return '账号已被禁用,请联系管理员'
  if (status === 429) return '请求过于频繁,请稍后重试'
  return text || '验证失败,请稍后重试'
}

// isLoginMFASessionLost marks the statuses where the handoff cannot be retried
// with the token we hold. 403 (account disabled mid-challenge) destroys it
// server-side; 401 MAY have destroyed it (expiry / budget) but may also be a
// plain typo, so it is NOT included - the view keeps 401 retryable and instead
// tells the user how to start over.
export function isLoginMFASessionLost(err: unknown): boolean {
  return errorShape(err).status === 403
}
