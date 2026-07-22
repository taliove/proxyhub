<template>
  <span class="ph-wordmark">
    <span class="ph-wordmark__text">ProxyHub</span>
    <span class="ph-wordmark__cursor" aria-hidden="true"></span>
  </span>
</template>

<script setup lang="ts">
// ProxyHub 字标:等宽字体 + 主色光标方块(▮)收尾。
// 供侧边栏顶栏与登录页(0011)复用;字号消费 --ph-text-md,
// 宿主可通过 CSS font-size 覆盖(光标尺寸随 em 等比缩放)。
defineOptions({ name: 'PhWordmark' })
</script>

<style scoped>
.ph-wordmark {
  display: inline-flex;
  align-items: baseline;
  font-family:
    ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace;
  font-size: var(--ph-text-md);
  font-weight: 700;
  letter-spacing: 0.01em;
  color: var(--ph-text-primary);
  white-space: nowrap;
  user-select: none;
}

/* 光标方块:主色填充,宽 0.55em 高 1em,模拟终端光标闪烁 */
.ph-wordmark__cursor {
  width: 0.55em;
  height: 1em;
  margin-left: 2px;
  background: var(--ph-color-primary);
  border-radius: 1px;
  transform: translateY(0.12em);
  animation: ph-wordmark-blink 1.2s step-end infinite;
}

@keyframes ph-wordmark-blink {
  0%,
  55% {
    opacity: 1;
  }
  56%,
  100% {
    opacity: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .ph-wordmark__cursor {
    animation: none;
  }
}
</style>
