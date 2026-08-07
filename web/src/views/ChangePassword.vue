<template>
  <div class="change-password">
    <!-- 装饰背景:与登录页同族,渐变网格 + 漂浮光斑,明暗自适应 -->
    <div class="change-password__bg" aria-hidden="true">
      <span class="blob blob--1"></span>
      <span class="blob blob--2"></span>
      <span class="blob blob--3"></span>
      <div class="grid"></div>
    </div>

    <!-- 主题切换 -->
    <button
      class="change-password__theme"
      type="button"
      :title="isDark ? '切换浅色' : '切换深色'"
      @click="layout.toggleDark()"
    >
      <el-icon :size="18"><Moon v-if="!isDark" /><Sunny v-else /></el-icon>
    </button>

    <div class="change-password__card">
      <div class="brand">
        <Wordmark class="brand__wordmark" />
        <p class="brand__subtitle">修改密码</p>
      </div>

      <el-alert
        v-if="authStore.mustChangePassword"
        type="warning"
        :closable="false"
        class="change-password__alert"
        title="首次登录请修改密码"
      />

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        class="change-password__form"
        label-position="top"
        @submit.prevent="handleSubmit"
      >
        <el-form-item label="旧密码" prop="oldPassword">
          <el-input
            v-model="form.oldPassword"
            type="password"
            placeholder="请输入当前密码"
            size="large"
            autocomplete="current-password"
            show-password
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="form.newPassword"
            type="password"
            placeholder="至少 8 位，需同时含字母与数字"
            size="large"
            autocomplete="new-password"
            show-password
          />
        </el-form-item>
        <el-form-item label="确认新密码" prop="confirmPassword">
          <el-input
            v-model="form.confirmPassword"
            type="password"
            placeholder="再次输入新密码"
            size="large"
            autocomplete="new-password"
            show-password
            @keyup.enter="handleSubmit"
          />
        </el-form-item>
        <el-button
          type="primary"
          native-type="submit"
          :loading="loading"
          class="change-password__submit"
          size="large"
        >
          {{ loading ? '提交中…' : '确认修改' }}
        </el-button>
      </el-form>

      <p class="change-password__footer">修改成功后将退出登录，请使用新密码重新登录</p>
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
import { Moon, Sunny } from '@element-plus/icons-vue'
import Wordmark from '@/components/Wordmark.vue'
import client from '@/api/client'

const router = useRouter()
const authStore = useAuthStore()
const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)

const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

// 与后端 validateNewPassword 对齐:>= 8 位且同时含字母与数字。
// 前端只做体验层面的早期反馈,真正的校验权威在服务端。
const validateNewPassword = (_rule: unknown, value: string, callback: (e?: Error) => void) => {
  if (!value) {
    callback(new Error('请输入新密码'))
    return
  }
  if (value.length < 8) {
    callback(new Error('新密码至少 8 位'))
    return
  }
  if (!/[A-Za-z]/.test(value) || !/\d/.test(value)) {
    callback(new Error('新密码需同时包含字母与数字'))
    return
  }
  callback()
}

const validateConfirm = (_rule: unknown, value: string, callback: (e?: Error) => void) => {
  if (!value) {
    callback(new Error('请再次输入新密码'))
    return
  }
  if (value !== form.newPassword) {
    callback(new Error('两次输入的新密码不一致'))
    return
  }
  callback()
}

const rules: FormRules = {
  oldPassword: [{ required: true, message: '请输入旧密码', trigger: 'blur' }],
  newPassword: [{ validator: validateNewPassword, trigger: 'blur' }],
  confirmPassword: [{ validator: validateConfirm, trigger: 'blur' }]
}

const handleSubmit = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    await client.post('/me/password', {
      old_password: form.oldPassword,
      new_password: form.newPassword
    })
    // 后端已销毁会话并清除 must_change_password;本地同步清位,守卫即刻放行,
    // 之后任何业务请求 401,由 axios 拦截器统一送回登录页。
    authStore.clearMustChangePassword()
    ElMessage.success('密码已修改，请重新登录')
    router.push('/')
  } catch {
    // 错误由 axios 拦截器统一提示
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.change-password {
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
.change-password__bg {
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
.change-password__theme {
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
.change-password__theme:hover {
  color: var(--ph-primary);
  border-color: var(--ph-primary);
  transform: translateY(-1px);
}

/* ---------- 卡片 ---------- */
.change-password__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 420px;
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
.brand__wordmark {
  font-size: var(--ph-text-display-sm);
}
.brand__subtitle {
  margin: 0;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}

/* ---------- 提示 ---------- */
.change-password__alert {
  margin-bottom: var(--ph-space-5);
}

/* ---------- 表单 ---------- */
.change-password__form :deep(.el-form-item) {
  margin-bottom: 20px;
}
.change-password__form :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}
.change-password__submit {
  width: 100%;
  margin-top: var(--ph-space-1);
  font-weight: 600;
  letter-spacing: 2px;
  border-radius: var(--ph-radius);
}

.change-password__footer {
  margin: var(--ph-space-5) 0 0;
  text-align: center;
  font-size: 12px;
  color: var(--ph-text-secondary);
}

@media (max-width: 480px) {
  .change-password__card {
    padding: 32px 22px 22px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .blob,
  .change-password__card {
    animation: none;
  }
}
</style>
