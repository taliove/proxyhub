<template>
  <!-- 品牌图形标:hub 字形(中心环 + 三路辐条 + P 弧)内联 SVG,
       渐变与描边消费令牌,随品牌色/主题自动切换;em 尺寸宿主可控 -->
  <svg
    class="ph-brand-mark"
    viewBox="0 0 64 64"
    role="img"
    aria-label="ProxyHub"
    xmlns="http://www.w3.org/2000/svg"
  >
    <defs>
      <linearGradient id="ph-brand-grad" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" class="ph-brand-mark__stop-start" />
        <stop offset="1" class="ph-brand-mark__stop-end" />
      </linearGradient>
    </defs>
    <rect x="2" y="2" width="60" height="60" rx="14" fill="url(#ph-brand-grad)" />
    <g stroke="#fff" stroke-width="4" stroke-linecap="round" fill="none">
      <!-- 中心环 -->
      <circle cx="32" cy="32" r="6" />
      <!-- 左侧三路辐条 -->
      <line x1="16" y1="16" x2="26.5" y2="26.5" />
      <line x1="14" y1="32" x2="25" y2="32" />
      <line x1="16" y1="48" x2="26.5" y2="37.5" />
      <!-- P 弧:中心环顶部扫向右节点 -->
      <path d="M 32 26 A 11 11 0 0 1 51 32" />
      <!-- 中心到右节点横辐 -->
      <line x1="39" y1="32" x2="46" y2="32" />
    </g>
    <g fill="#fff">
      <circle cx="13" cy="13" r="4" />
      <circle cx="10" cy="32" r="4" />
      <circle cx="13" cy="51" r="4" />
      <circle cx="51" cy="32" r="4" />
    </g>
  </svg>
</template>

<style scoped>
.ph-brand-mark {
  width: 1em;
  height: 1em;
  display: block;
}

.ph-brand-mark__stop-start {
  stop-color: var(--ph-teal-400);
}

.ph-brand-mark__stop-end {
  stop-color: var(--ph-teal-700);
}
</style>
