<template>
  <div class="auth-shell">
    <!-- 装饰背景:渐变网格 + 漂浮光斑,明暗自适应 -->
    <div class="auth-shell__bg" aria-hidden="true">
      <span class="blob blob--1"></span>
      <span class="blob blob--2"></span>
      <span class="blob blob--3"></span>
      <div class="grid"></div>
    </div>

    <!-- 主题切换 -->
    <button
      class="auth-shell__theme"
      type="button"
      :title="isDark ? '切换浅色' : '切换深色'"
      @click="layout.toggleDark()"
    >
      <el-icon :size="18"><Moon v-if="!isDark" /><Sunny v-else /></el-icon>
    </button>

    <div class="auth-shell__card">
      <div class="brand">
        <Wordmark class="brand__wordmark" />
        <p class="brand__subtitle">{{ subtitle }}</p>
      </div>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
// AuthShell 是免登录/强制流程页(登录、改密、MFA 绑定)的外壳:
// 装饰背景 + 主题切换 + 玻璃卡片 + 品牌头。这些样式此前在每个页面里各抄一份,
// 新页(ticket 08)改为复用本组件,老页保持原样以免牵动其既有测试。
import { storeToRefs } from 'pinia'
import { Moon, Sunny } from '@element-plus/icons-vue'
import { useLayoutStore } from '@/stores/layout'
import Wordmark from '@/components/Wordmark.vue'

defineProps<{
  // subtitle 是品牌名下的一行说明,由具体页面给出。
  subtitle: string
}>()

const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)
</script>

<style scoped>
.auth-shell {
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
.auth-shell__bg {
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
.auth-shell__theme {
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
.auth-shell__theme:hover {
  color: var(--ph-primary);
  border-color: var(--ph-primary);
  transform: translateY(-1px);
}

/* ---------- 卡片 ---------- */
.auth-shell__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 460px;
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

@media (max-width: 480px) {
  .auth-shell__card {
    padding: 32px 22px 22px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .blob,
  .auth-shell__card {
    animation: none;
  }
}
</style>
