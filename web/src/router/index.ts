import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

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
        meta: { title: '我的订阅', icon: 'Link', group: 'resources' }
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
        path: 'audit',
        name: 'Audit',
        component: () => import('@/views/Audit.vue'),
        meta: { title: '安全审计', icon: 'Warning', group: 'config' }
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
        meta: { title: '系统设置', icon: 'Setting', group: 'config' }
      }
    ]
  },
  {
    path: '/self-nodes',
    redirect: '/nodes?source=self-hosted'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()

  if (to.meta.skipAuth) {
    next()
  } else if (!authStore.isAuthenticated) {
    next('/login')
  } else {
    next()
  }
})

export default router
