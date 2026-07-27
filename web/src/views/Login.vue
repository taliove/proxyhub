<template>
  <div class="login">
    <!-- 装饰背景:渐变网格 + 漂浮光斑,明暗自适应 -->
    <div class="login__bg" aria-hidden="true">
      <span class="blob blob--1"></span>
      <span class="blob blob--2"></span>
      <span class="blob blob--3"></span>
      <div class="grid"></div>
    </div>

    <!-- 主题切换 -->
    <button
      class="login__theme"
      type="button"
      :title="isDark ? '切换浅色' : '切换深色'"
      @click="layout.toggleDark()"
    >
      <el-icon :size="18"><Moon v-if="!isDark" /><Sunny v-else /></el-icon>
    </button>

    <div class="login__card">
      <div class="brand">
        <Wordmark class="brand__wordmark" />
        <p class="brand__subtitle">代理订阅聚合系统</p>
      </div>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        class="login__form"
        @submit.prevent="handleLogin"
      >
        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            placeholder="用户名"
            size="large"
            autocomplete="username"
            clearable
          >
            <template #prefix
              ><el-icon><User /></el-icon
            ></template>
          </el-input>
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            size="large"
            autocomplete="current-password"
            show-password
            @keyup.enter="handleLogin"
          >
            <template #prefix
              ><el-icon><Lock /></el-icon
            ></template>
          </el-input>
        </el-form-item>
        <el-button
          type="primary"
          native-type="submit"
          :loading="loading"
          class="login__submit"
          size="large"
        >
          {{ loading ? '登录中…' : '登 录' }}
        </el-button>
      </el-form>

      <p class="login__footer">ProxyHub · 安全高效的订阅聚合</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { User, Lock, Moon, Sunny } from '@element-plus/icons-vue'
import Wordmark from '@/components/Wordmark.vue'
import client from '@/api/client'

const router = useRouter()
const authStore = useAuthStore()
const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)

const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  username: '',
  password: ''
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
}

const handleLogin = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    // Login response carries the user profile (ticket 02); role drives admin UI gating,
    // must_change_password (ticket 04) routes the user to the forced password change.
    const data = await client.post<
      unknown,
      { role?: string; user?: { role?: string; must_change_password?: boolean } }
    >('/login', form)
    const role = data?.user?.role ?? data?.role ?? ''
    const mustChange = data?.user?.must_change_password ?? false
    authStore.setAuth(form.username, role, mustChange)
    if (mustChange) {
      router.push('/change-password')
      return
    }
    ElMessage.success('登录成功')
    router.push('/')
  } catch {
    // 错误由 axios 拦截器统一提示
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  overflow: hidden;
  background: var(--ph-bg-page);
}

/* ---------- 装饰背景 ---------- */
.login__bg {
  position: absolute;
  inset: 0;
  z-index: 0;
}

.grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(to right, var(--ph-border-light) 1px, transparent 1px),
    linear-gradient(to bottom, var(--ph-border-light) 1px, transparent 1px);
  background-size: 42px 42px;
  mask-image: radial-gradient(circle at 50% 45%, #000 0%, transparent 75%);
  -webkit-mask-image: radial-gradient(circle at 50% 45%, #000 0%, transparent 75%);
  opacity: 0.6;
}

.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(64px);
  opacity: 0.5;
  will-change: transform;
}
.blob--1 {
  width: 460px;
  height: 460px;
  top: -120px;
  left: -100px;
  background: radial-gradient(circle, var(--ph-color-primary), transparent 70%);
  animation: drift 18s ease-in-out infinite;
}
.blob--2 {
  width: 380px;
  height: 380px;
  bottom: -140px;
  right: -80px;
  background: radial-gradient(circle, var(--ph-color-primary-hover), transparent 70%);
  animation: drift 22s ease-in-out infinite reverse;
  opacity: 0.35;
}
.blob--3 {
  width: 300px;
  height: 300px;
  top: 40%;
  right: 18%;
  background: radial-gradient(circle, var(--ph-color-primary-active), transparent 70%);
  animation: drift 26s ease-in-out infinite;
  opacity: 0.25;
}

@keyframes drift {
  0%,
  100% {
    transform: translate(0, 0) scale(1);
  }
  33% {
    transform: translate(40px, -30px) scale(1.08);
  }
  66% {
    transform: translate(-30px, 24px) scale(0.94);
  }
}

/* ---------- 主题切换 ---------- */
.login__theme {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 2;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius);
  background: var(--ph-bg-surface);
  color: var(--ph-text-regular);
  cursor: pointer;
  box-shadow: var(--ph-shadow-sm);
  transition:
    color var(--ph-transition),
    border-color var(--ph-transition),
    transform var(--ph-transition);
}
.login__theme:hover {
  color: var(--ph-primary);
  border-color: var(--ph-primary);
  transform: translateY(-1px);
}
/* ---------- 卡片 ---------- */
.login__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 400px;
  padding: 40px 36px 28px;
  border-radius: var(--ph-radius-lg);
  border: 1px solid var(--ph-border-light);
  background: color-mix(in srgb, var(--ph-bg-surface) 82%, transparent);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  box-shadow: var(--ph-shadow-lg);
  animation: rise 0.5s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(16px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* ---------- 品牌 ---------- */
.brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-6);
}
/* 门面场景字号放大:光标尺寸随 em 等比缩放 */
.brand__wordmark {
  font-size: var(--ph-text-display-sm);
}
.brand__subtitle {
  margin: 0;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}

/* ---------- 表单 ---------- */
.login__form :deep(.el-form-item) {
  margin-bottom: 22px;
}
.login__form :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}
.login__submit {
  width: 100%;
  margin-top: var(--ph-space-1);
  font-weight: 600;
  letter-spacing: 2px;
  border-radius: var(--ph-radius);
}

.login__footer {
  margin: var(--ph-space-5) 0 0;
  text-align: center;
  font-size: 12px;
  color: var(--ph-text-secondary);
}

@media (max-width: 480px) {
  .login__card {
    padding: 32px 22px 22px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .blob,
  .login__card {
    animation: none;
  }
}
</style>
