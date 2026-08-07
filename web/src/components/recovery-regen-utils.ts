// 恢复码重新生成的错误文案映射,与 .vue 分离以便单测(同 views/mfa-enroll-utils.ts)。
// 后端 handleMFARegenerateRecovery 写的是英文哨兵串,这里翻成可操作的中文。

function errorBody(err: unknown): { status?: number; error?: string } {
  const res = (err as { response?: { status?: number; data?: unknown } })?.response
  const data = res?.data as { error?: string; message?: string } | undefined
  return { status: res?.status, error: data?.error ?? data?.message }
}

/**
 * regenerateErrorMessage 本地化 POST /me/mfa/regenerate-recovery 的失败。
 * 400 有三种语义完全不同的情况:缺码、码不对、账号还没绑定过 MFA。
 */
export function regenerateErrorMessage(err: unknown): string {
  const { status, error } = errorBody(err)
  const body = error ?? ''
  if (status === 400) {
    if (body.includes('not enrolled')) {
      return '该账号还未绑定验证器，首批恢复码在绑定流程中发放'
    }
    if (body.includes('required')) {
      return '请输入动态码或恢复码'
    }
    return '确认码不正确或已过期，请用认证器上的最新动态码重试'
  }
  return '重新生成失败，请稍后重试'
}
