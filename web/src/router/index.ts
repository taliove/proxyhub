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
        component: () => import('@/views/Dashboard.vue'),
        meta: { title: '仪表盘', icon: 'Monitor' }
      },
      {
        path: 'airports',
        name: 'Airports',
        component: () => import('@/views/Airports.vue'),
        meta: { title: '机场管理', icon: 'Connection' }
      },
      {
        path: 'nodes',
        name: 'Nodes',
        component: () => import('@/views/nodes/index.vue'),
        meta: { title: '节点管理', icon: 'Cpu' }
      },
      {
        path: 'endpoints',
        name: 'Endpoints',
        component: () => import('@/views/Endpoints.vue'),
        meta: { title: '我的订阅', icon: 'Link' }
      },
      {
        path: 'template',
        name: 'Template',
        component: () => import('@/views/TemplateEditor.vue'),
        meta: { title: '订阅模板', icon: 'Document' }
      },
      {
        path: 'stats',
        name: 'Stats',
        component: () => import('@/views/Stats.vue'),
        meta: { title: '流量统计', icon: 'TrendCharts', divider: true }
      },
      {
        path: 'refresh-log',
        name: 'RefreshLog',
        component: () => import('@/views/refresh-log/index.vue'),
        meta: { title: '同步日志', icon: 'Refresh' }
      },
      {
        path: 'audit',
        name: 'Audit',
        component: () => import('@/views/Audit.vue'),
        meta: { title: '安全审计', icon: 'Warning' }
      },
      {
        path: 'settings',
        name: 'Settings',
        component: () => import('@/views/Settings.vue'),
        meta: { title: '系统设置', icon: 'Setting', divider: true }
      }
    ]
  },
  {
    path: '/self-nodes',
    redirect: '/nodes?tab=self'
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
