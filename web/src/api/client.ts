import axios from 'axios'
import { ElMessage } from 'element-plus'

declare module 'axios' {
  export interface AxiosRequestConfig {
    // 跳过 401 时的全局登录跳转（用于启动时的会话探测）
    skipAuthRedirect?: boolean
  }
}

const client = axios.create({
  baseURL: '/api',
  timeout: 30000,
  withCredentials: true
})

client.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const status = error.response?.status
    const skipAuthRedirect = error.config?.skipAuthRedirect
    if (status === 401) {
      if (!skipAuthRedirect) {
        window.location.href = '/login'
      }
    } else if (status !== 409) {
      ElMessage.error(error.response?.data?.message || '请求失败')
    }
    return Promise.reject(error)
  }
)

export default client
