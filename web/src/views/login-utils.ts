// Login error interpretation helpers.
//
// The login request runs with skipErrorToast + skipAuthRedirect (see
// src/api/auth.ts), so the view owns every message. Backend bodies are not
// uniform: the captcha wall and the "captcha now required" 401 write JSON
// ({error, captcha_required}), while ban / disabled / bad-request paths use
// http.Error and therefore write a plain-text body.

// errorBody normalizes an axios error into {status, text, captchaRequired}.
interface LoginErrorShape {
  status?: number
  text: string
  captchaRequired: boolean
}

function errorShape(err: unknown): LoginErrorShape {
  const res = (err as { response?: { status?: number; data?: unknown } } | null)?.response
  const data = res?.data
  if (typeof data === 'string') {
    return { status: res?.status, text: data.trim(), captchaRequired: false }
  }
  const body = (data ?? {}) as { error?: string; message?: string; captcha_required?: boolean }
  return {
    status: res?.status,
    text: (body.error ?? body.message ?? '').trim(),
    captchaRequired: body.captcha_required === true
  }
}

// captchaRequiredFromError reports whether the backend flagged this IP as
// needing a captcha from now on. Both wrong-password and wrong-captcha 401s
// can carry the flag, so the caller must not assume which one it was.
export function captchaRequiredFromError(err: unknown): boolean {
  return errorShape(err).captchaRequired
}

// loginErrorMessage maps a failed login onto an actionable Chinese message.
export function loginErrorMessage(err: unknown): string {
  const { status, text, captchaRequired } = errorShape(err)
  if (status === undefined) return '网络异常,请检查连接后重试'
  if (status === 401) {
    // "captcha required" is the captcha gate rejecting a missing/wrong answer;
    // anything else at 401 is a credential failure.
    if (text.includes('captcha')) return '验证码错误或已过期,请重新输入'
    return captchaRequired ? '用户名或密码错误,请输入验证码后重试' : '用户名或密码错误'
  }
  if (status === 403) {
    if (text.includes('disabled')) return '账号已被禁用,请联系管理员'
    if (text.includes('too many')) return '失败次数过多,该 IP 已被临时封禁,请稍后重试'
  }
  if (status === 429) return '请求过于频繁,请稍后重试'
  return text || '登录失败,请稍后重试'
}
