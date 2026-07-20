import { defineStore } from 'pinia'
import { ref } from 'vue'
import client from '@/api/client'

export const useAuthStore = defineStore('auth', () => {
  const isAuthenticated = ref(false)
  const username = ref('')

  function setAuth(user: string) {
    isAuthenticated.value = true
    username.value = user
  }

  function clearAuth() {
    isAuthenticated.value = false
    username.value = ''
  }

  // restore 用服务器端会话 cookie 恢复登录态（刷新页面后调用）
  async function restore(): Promise<boolean> {
    try {
      const data = await client.get<unknown, { username: string }>('/me', {
        skipAuthRedirect: true
      })
      setAuth(data.username)
      return true
    } catch {
      clearAuth()
      return false
    }
  }

  return { isAuthenticated, username, setAuth, clearAuth, restore }
})
