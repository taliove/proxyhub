<template>
  <!-- 24 小时探测网格(issue #103):24 格,左旧右新;色块语义 全通/部分通/全断/无数据 -->
  <span class="probe-grid">
    <el-tooltip v-for="(v, i) in grid" :key="i" :content="tooltip(i, v)" placement="top">
      <span class="probe-cell" :class="cellClass(v)" />
    </el-tooltip>
  </span>
</template>

<script setup lang="ts">
defineProps<{ grid: number[] }>()

// grid[23] 是当前小时,左起第 i 格是 23-i 小时前
const tooltip = (i: number, v: number): string => {
  const label = v === 1 ? '全通' : v === 3 ? '全断' : v === 2 ? '部分通' : '无数据'
  const ago = 23 - i
  const when = ago === 0 ? '当前小时' : `${ago} 小时前`
  return `${when} · ${label}`
}

const cellClass = (v: number) =>
  v === 1 ? 'cell-ok' : v === 3 ? 'cell-down' : v === 2 ? 'cell-mixed' : 'cell-none'
</script>

<style scoped>
.probe-grid {
  display: inline-flex;
  gap: 2px;
}
.probe-cell {
  width: 8px;
  height: 14px;
  border-radius: var(--ph-radius-sm);
  display: inline-block;
}
.cell-ok {
  background: var(--ph-success);
}
.cell-down {
  background: var(--ph-danger);
}
.cell-mixed {
  background: var(--ph-warning);
}
.cell-none {
  background: var(--ph-border-light);
}
</style>
