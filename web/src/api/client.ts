import axios from 'axios'
import { ElMessage } from 'element-plus'

declare module 'axios' {
  export interface AxiosRequestConfig {
    // 跳过 401 时的全局登录跳转（用于启动时的会话探测）
    skipAuthRedirect?: boolean
    // 跳过全局错误 toast（调用方自行处理错误展示,如本地化文案）
    skipErrorToast?: boolean
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
    const body = error.response?.data
    if (status === 401) {
      if (!skipAuthRedirect) {
        window.location.href = '/login'
      }
    } else if (status === 403 && body?.must_change_password === true) {
      // 首登强改密(ticket 04):任何业务请求被 requirePasswordChanged 拦截时,
      // 前端直接跳改密页;改密成功后后端销毁会话,401 拦截器再送用户回登录页。
      // 已在改密页时不再重复跳转,避免 POST /me/password 失败(如旧密码错)时死循环。
      if (!window.location.pathname.startsWith('/change-password')) {
        window.location.href = '/change-password'
      }
    } else if (status !== 409 && !error.config?.skipErrorToast) {
      // 后端错误字段不统一:多数处理器写 {message},用户管理面写 {error}。
      ElMessage.error(body?.message || body?.error || '请求失败')
    }
    return Promise.reject(error)
  }
)

export default client
