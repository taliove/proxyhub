<template>
  <div class="setup">
    <!-- 装饰背景:与登录页同族,渐变网格 + 漂浮光斑,明暗自适应 -->
    <div class="setup__bg" aria-hidden="true">
      <span class="blob blob--1"></span>
      <span class="blob blob--2"></span>
      <span class="blob blob--3"></span>
      <div class="grid"></div>
    </div>

    <!-- 主题切换 -->
    <button
      class="setup__theme"
      type="button"
      :title="isDark ? '切换浅色' : '切换深色'"
      @click="layout.toggleDark()"
    >
      <el-icon :size="18"><Moon v-if="!isDark" /><Sunny v-else /></el-icon>
    </button>

    <div class="setup__card">
      <div class="brand">
        <Wordmark class="brand__wordmark" />
        <p class="brand__subtitle">系统初始化</p>
      </div>

      <el-steps :active="step" finish-status="success" align-center>
        <el-step title="管理员账户" />
        <el-step title="安全策略" />
        <el-step title="完成" />
      </el-steps>

      <div class="step-content">
        <!-- 步骤 1: 管理员账户 -->
        <div v-if="step === 0">
          <el-form :model="form" label-width="120px">
            <el-alert type="warning" :closable="false" class="step-alert">
              为安全起见，禁止使用 "admin" 作为用户名
            </el-alert>
            <el-form-item label="用户名">
              <el-input v-model="form.username" placeholder="请输入用户名（非 admin）" />
            </el-form-item>
            <el-form-item label="密码">
              <el-input
                v-model="form.password"
                type="password"
                placeholder="至少 8 位"
                show-password
              />
            </el-form-item>
            <el-form-item label="确认密码">
              <el-input
                v-model="form.confirmPassword"
                type="password"
                placeholder="再次输入密码"
                show-password
              />
            </el-form-item>
          </el-form>
        </div>

        <!-- 步骤 2: 安全策略 -->
        <div v-if="step === 1">
          <el-form :model="form" label-width="180px">
            <el-form-item label="登录失败封禁阈值">
              <el-input-number v-model="form.banThreshold" :min="3" :max="10" />
              <span class="form-tip">连续失败此次数后封禁 IP</span>
            </el-form-item>
            <el-form-item label="封禁时长（小时）">
              <el-input-number v-model="form.banDuration" :min="1" :max="24" />
            </el-form-item>
          </el-form>
        </div>

        <!-- 步骤 3: 完成 -->
        <div v-if="step === 2">
          <el-result icon="success" title="初始化完成！">
            <template #sub-title>
              <p>系统已完成初始化，即将跳转到登录页面</p>
            </template>
          </el-result>
        </div>
      </div>

      <div class="step-actions">
        <el-button v-if="step > 0 && step < 2" @click="step--">上一步</el-button>
        <el-button v-if="step < 2" type="primary" @click="nextStep">下一步</el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useLayoutStore } from '@/stores/layout'
import { ElMessage } from 'element-plus'
import { Moon, Sunny } from '@element-plus/icons-vue'
import Wordmark from '@/components/Wordmark.vue'
import client from '@/api/client'

const router = useRouter()
const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)
const step = ref(0)

const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  banThreshold: 5,
  banDuration: 1
})

const nextStep = async () => {
  if (step.value === 0) {
    if (form.username === 'admin') {
      ElMessage.error('禁止使用 admin 作为用户名')
      return
    }
    if (!form.username || form.username.length < 3) {
      ElMessage.error('用户名至少 3 位')
      return
    }
    if (form.password.length < 8) {
      ElMessage.error('密码至少 8 位')
      return
    }
    if (form.password !== form.confirmPassword) {
      ElMessage.error('两次密码不一致')
      return
    }
    step.value++
  } else if (step.value === 1) {
    try {
      await client.post('/setup', {
        username: form.username,
        password: form.password,
        security: {
          ban_threshold: form.banThreshold,
          ban_duration: `${form.banDuration}h`
        }
      })
      step.value++
      setTimeout(() => router.push('/login'), 2000)
    } catch {
      ElMessage.error('初始化失败')
    }
  }
}
</script>

<style scoped>
.setup {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--ph-space-5);
  overflow: hidden;
  background: var(--ph-bg-page);
}

/* ---------- 装饰背景(与登录页同族) ---------- */
.setup__bg {
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
.setup__theme {
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
.setup__theme:hover {
  color: var(--ph-primary);
  border-color: var(--ph-primary);
  transform: translateY(-1px);
}

/* ---------- 卡片(与登录页同族) ---------- */
.setup__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 640px;
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

/* ---------- 品牌(与登录页同族) ---------- */
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

.step-alert {
  margin-bottom: var(--ph-space-5);
}

.step-content {
  margin: var(--ph-space-7) 0;
  min-height: 300px;
}

.step-actions {
  text-align: center;
  margin-top: var(--ph-space-6);
}

.form-tip {
  margin-left: var(--ph-space-2);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}

@media (max-width: 480px) {
  .setup__card {
    padding: 32px 22px 22px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .blob,
  .setup__card {
    animation: none;
  }
}
</style>
