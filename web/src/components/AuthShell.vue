<template>
  <div class="auth-shell">
    <!-- 仪器底版:纸灰底 + 细网格,网格随径向淡出;不放光斑/渐变块,主角只有光标字标 -->
    <div class="auth-shell__bg" aria-hidden="true">
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

    <div class="auth-shell__card" :class="{ 'auth-shell__card--wide': wide }">
      <div class="brand">
        <Wordmark class="brand__wordmark" />
        <p class="brand__subtitle">{{ subtitle }}</p>
      </div>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
// AuthShell 是免登录/强制流程页(登录、改密、MFA 绑定、Setup)的外壳:
// 仪器底版 + 主题切换 + 白卡片 + 品牌头。DESIGN.md 明令禁止渐变光斑与玻璃拟态,
// 所有认证页统一走本组件,不允许页面自抄一份外壳样式。
import { storeToRefs } from 'pinia'
import { Moon, Sunny } from '@element-plus/icons-vue'
import { useLayoutStore } from '@/stores/layout'
import Wordmark from '@/components/Wordmark.vue'

defineProps<{
  // subtitle 是品牌名下的一行说明,由具体页面给出。
  subtitle: string
  // wide 给 Setup 这类含步骤条/宽表单的页:卡片 460px → 640px。
  wide?: boolean
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

/* ---------- 仪器底版 ---------- */
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
  /* mask 只看 alpha 通道:取任一全透明度过渡即可,text-primary 是不透明语义令牌(明暗色皆然) */
  mask-image: radial-gradient(circle at 50% 45%, var(--ph-text-primary) 0%, transparent 75%);
  -webkit-mask-image: radial-gradient(
    circle at 50% 45%,
    var(--ph-text-primary) 0%,
    transparent 75%
  );
  opacity: 0.6;
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

/* ---------- 卡片:仪器表面白,静置微影,无玻璃拟态 ---------- */
.auth-shell__card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 460px;
  padding: 40px 36px 28px;
  border-radius: var(--ph-radius-lg);
  border: 1px solid var(--ph-border-light);
  background: var(--ph-bg-surface);
  box-shadow: var(--ph-shadow-sm);
  animation: rise 0.5s cubic-bezier(0.16, 1, 0.3, 1);
}
.auth-shell__card--wide {
  max-width: 640px;
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
  .auth-shell__card {
    animation: none;
  }
}
</style>
