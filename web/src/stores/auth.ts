import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import client from '@/api/client'

// MeResponse mirrors GET /api/me payload (ticket 02): role drives admin UI gating,
// must_change_password drives the forced first-login password-change flow (ticket 04).
interface MeResponse {
  username: string
  role?: string
  must_change_password?: boolean
  acting?: boolean
}

// ActingUser describes the impersonation target when a super admin has
// entered another user's space (ticket 09). Null when not impersonating.
export interface ActingUser {
  id: number
  username: string
}

export const useAuthStore = defineStore('auth', () => {
  const isAuthenticated = ref(false)
  const username = ref('')
  // role comes from /me (or login response); empty until restored.
  const role = ref('')
  // mustChangePassword comes from login/me; when true the router guard forces
  // the user onto /change-password before any business page.
  const mustChangePassword = ref(false)
  // actingUser holds the impersonation target while a super admin is inside
  // another user's space; the navbar shows "viewing as X" with an exit button.
  const actingUser = ref<ActingUser | null>(null)

  // isSuperAdmin gates the admin-only routes and nav entries.
  const isSuperAdmin = computed(() => role.value === 'super_admin')

  function setAuth(user: string, userRole = '', mustChange = false) {
    isAuthenticated.value = true
    username.value = user
    role.value = userRole
    mustChangePassword.value = mustChange
  }

  function setActingUser(target: ActingUser | null) {
    actingUser.value = target
  }

  function clearAuth() {
    isAuthenticated.value = false
    username.value = ''
    role.value = ''
    mustChangePassword.value = false
    actingUser.value = null
  }

  // clearMustChangePassword flips the flag after a successful password change so
  // the guard stops redirecting; the session itself is destroyed server-side and
  // the user will be sent back through /login by the 401 interceptor.
  function clearMustChangePassword() {
    mustChangePassword.value = false
  }

  // restore 用服务器端会话 cookie 恢复登录态（刷新页面后调用）
  async function restore(): Promise<boolean> {
    try {
      const data = await client.get<unknown, MeResponse>('/me', {
        skipAuthRedirect: true
      })
      setAuth(data.username, data.role ?? '', data.must_change_password ?? false)
      return true
    } catch {
      clearAuth()
      return false
    }
  }

  return {
    isAuthenticated,
    username,
    role,
    mustChangePassword,
    isSuperAdmin,
    actingUser,
    setAuth,
    setActingUser,
    clearAuth,
    clearMustChangePassword,
    restore
  }
})
