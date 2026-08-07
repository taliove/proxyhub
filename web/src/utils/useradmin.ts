// Admin user-management view helpers (Users.vue): credential generation,
// clipboard copy, and localized create-error toasts.
import { ElMessage } from 'element-plus'

// generatePassword returns a 16-char alphanumeric random password using
// crypto.getRandomValues (CSPRNG; never Math.random for credentials).
export function generatePassword(): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
  const bytes = new Uint32Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('')
}

// copyPassword writes the credential to the clipboard, with a fallback hint.
export async function copyPassword(pwd: string) {
  try {
    await navigator.clipboard.writeText(pwd)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

// toastCreateUserError maps the known create-user backend reasons (store
// users.go sentinels) onto actionable Chinese messages. createUser runs with
// skipErrorToast so the interceptor stays silent and this is the only toast.
export function toastCreateUserError(err: unknown, username: string) {
  const status = (err as { response?: { status?: number } })?.response?.status
  const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? ''
  if (status === 409) {
    ElMessage.error(`用户名「${username}」已被占用，请更换`)
  } else if (msg.includes('reserved')) {
    ElMessage.error(`用户名「${username}」为系统保留名（admin/root/guest 等）,请更换`)
  } else if (msg) {
    ElMessage.error(msg)
  } else {
    ElMessage.error('创建失败，请稍后重试')
  }
}
