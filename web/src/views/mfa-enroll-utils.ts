// Pure helpers for the MFA enrollment view (ticket 08). Kept out of the .vue
// so the wire-error mapping and the display formatting are unit-testable
// without mounting Element Plus - same split as views/login-utils.ts.

// TOTP_CODE_LENGTH mirrors the backend's 6-digit TOTP (handlers_mfa.go stages
// digits: 6). The view uses it for both the maxlength and the submit gate.
export const TOTP_CODE_LENGTH = 6

// normalizeTOTPCode strips everything that is not a digit and truncates to the
// expected length. Authenticator apps display codes as "123 456", and pasting
// that (or a stray space) must not fail verification for a formatting reason.
export function normalizeTOTPCode(raw: string): string {
  return (raw ?? '').replace(/\D/g, '').slice(0, TOTP_CODE_LENGTH)
}

// isCompleteTOTPCode reports whether the code is ready to submit.
export function isCompleteTOTPCode(raw: string): boolean {
  return normalizeTOTPCode(raw).length === TOTP_CODE_LENGTH
}

// formatSecretForDisplay groups the base32 secret into 4-char blocks so it can
// be read off the screen and typed into an authenticator by hand without
// losing the place. Purely cosmetic: copy actions use the raw secret.
export function formatSecretForDisplay(secret: string): string {
  const compact = (secret ?? '').replace(/\s+/g, '')
  if (compact === '') return ''
  return (compact.match(/.{1,4}/g) ?? []).join(' ')
}

// errorBody digs the JSON body out of an axios error without pulling axios
// types in; the backend writes {error: "..."} for MFA failures.
function errorBody(err: unknown): { status?: number; error?: string } {
  const res = (err as { response?: { status?: number; data?: unknown } })?.response
  const data = res?.data as { error?: string; message?: string } | undefined
  return { status: res?.status, error: data?.error ?? data?.message }
}

// enrollStartErrorMessage localizes stage-one failures. 409 is the meaningful
// one: MFA is already bound, so the guard should have let the user through -
// the view treats it as "you are done" rather than a dead end.
export function enrollStartErrorMessage(err: unknown): string {
  const { status } = errorBody(err)
  if (status === 409) return '该账号已绑定过验证器，无需重复绑定'
  return '获取绑定密钥失败，请刷新页面重试'
}

// isAlreadyEnrolled marks the 409 "mfa already enrolled" case, which means the
// local must_enroll_mfa flag is stale and the user should just be released.
export function isAlreadyEnrolled(err: unknown): boolean {
  return errorBody(err).status === 409
}

// enrollConfirmErrorMessage localizes stage-two failures. The two 400 bodies
// are distinct problems: a wrong/expired code (retry) versus a missing staged
// secret (restart stage one, usually after a server-side reset).
export function enrollConfirmErrorMessage(err: unknown): string {
  const { status, error } = errorBody(err)
  if (status === 409) return '该账号已绑定过验证器，无需重复绑定'
  if (status === 400) {
    if ((error ?? '').includes('no pending enrollment')) {
      return '绑定已失效，请重新获取密钥'
    }
    return '验证码不正确或已过期，请用认证器上的最新 6 位码重试'
  }
  return '绑定失败，请稍后重试'
}
