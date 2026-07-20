import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@/styles/index.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
import { useAuthStore } from '@/stores/auth'
import './monaco-worker'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(ElementPlus)

for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

// 挂载前先用服务器端会话恢复登录态，避免刷新后被路由守卫踢回登录页
async function bootstrap() {
  const authStore = useAuthStore()
  await authStore.restore()
  app.use(router)
  app.mount('#app')
}

bootstrap()
