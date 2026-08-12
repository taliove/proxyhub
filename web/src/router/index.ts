import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { appBase } from '@/utils/base'
import { resolveRedirect } from './guard'

const routes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('@/views/Setup.vue'),
    meta: { skipAuth: true }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { skipAuth: true }
  },
  {
    // 首登强制改密与自助改密(ticket 04);要求已登录,但豁免 mustChangePassword 重定向
    path: '/change-password',
    name: 'ChangePassword',
    component: () => import('@/views/ChangePassword.vue')
  },
  {
    // MFA 强制绑定(ticket 08);要求已登录,但豁免 mustEnrollMFA 重定向。
    // 后端对 /api/me/mfa/enroll 同样豁免 requireMFAEnrolled,两边保持一致。
    path: '/mfa/enroll',
    name: 'MFAEnroll',
    component: () => import('@/views/MFAEnroll.vue')
  },
  {
    path: '/',
    component: () => import('@/layout/index.vue'),
    children: [
      {
        path: '',
        name: 'Dashboard',
        component: () => import('@/views/dashboard/index.vue'),
        meta: { title: '仪表盘', icon: 'Monitor', group: 'overview' }
      },
      {
        path: 'speedtest',
        name: 'Speedtest',
        component: () => import('@/views/speedtest/index.vue'),
        meta: { title: '本机实测', icon: 'Odometer', group: 'overview' }
      },
      {
        path: 'airports',
        name: 'Airports',
        component: () => import('@/views/Airports.vue'),
        meta: { title: '机场管理', icon: 'Connection', group: 'resources' }
      },
      {
        path: 'nodes',
        name: 'Nodes',
        component: () => import('@/views/nodes/index.vue'),
        meta: { title: '节点管理', icon: 'Cpu', group: 'resources' }
      },
      {
        path: 'endpoints',
        name: 'Endpoints',
        component: () => import('@/views/Endpoints.vue'),
        meta: { title: '订阅地址', icon: 'Link', group: 'resources' }
      },
      {
        path: 'template',
        name: 'Template',
        component: () => import('@/views/TemplateEditor.vue'),
        meta: { title: '订阅模板', icon: 'Document', group: 'config' }
      },
      {
        // 独立流量统计入口已撤(ticket 0010):统计图表并入仪表盘,旧链接重定向
        path: 'stats',
        redirect: '/'
      },
      {
        // 独立刷新日志入口已撤(ticket 05):刷新历史并入任务中心,旧链接重定向
        path: 'refresh-log',
        redirect: 'jobs'
      },
      {
        // 安全审计(登录/封禁事件流水 + 解封 IP):超管专属(后端 adminGuard 同样 403)
        path: 'audit',
        name: 'Audit',
        component: () => import('@/views/Audit.vue'),
        meta: { title: '安全审计', icon: 'Warning', group: 'system', requiresSuperAdmin: true }
      },
      {
        path: 'jobs',
        name: 'Jobs',
        component: () => import('@/views/jobs/index.vue'),
        meta: { title: '任务中心', icon: 'List', group: 'config' }
      },
      {
        path: 'settings',
        name: 'Settings',
        component: () => import('@/views/Settings.vue'),
        meta: { title: '系统设置', icon: 'Setting', group: 'system' }
      },
      {
        // Admin-only: user management (ticket 05); hidden from nav for non-super-admin
        path: 'admin/users',
        name: 'AdminUsers',
        component: () => import('@/views/admin/Users.vue'),
        meta: { title: '用户管理', icon: 'User', group: 'system', requiresSuperAdmin: true }
      }
    ]
  },
  {
    path: '/self-nodes',
    redirect: '/nodes?source=self-hosted'
  }
]

const router = createRouter({
  history: createWebHistory(appBase() || '/'),
  routes
})

router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()

  // 规则本体在 guard.ts(纯函数,可单测);这里只做 store -> 规则的适配。
  const redirect = resolveRedirect(
    {
      path: to.path,
      skipAuth: to.meta.skipAuth as boolean | undefined,
      requiresSuperAdmin: to.meta.requiresSuperAdmin as boolean | undefined
    },
    {
      isAuthenticated: authStore.isAuthenticated,
      mustChangePassword: authStore.mustChangePassword,
      mustEnrollMFA: authStore.mustEnrollMFA,
      isSuperAdmin: authStore.isSuperAdmin
    }
  )

  if (redirect) next(redirect)
  else next()
})

export default router
