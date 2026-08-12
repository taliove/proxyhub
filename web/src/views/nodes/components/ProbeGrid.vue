<template>
  <!-- 24 小时探测网格(issue #103):24 格小矩形,左旧右新;色块语义 全通/部分通/全断/无数据。
       hover 有 stats 时显示「探测 N 次,成功 M 次」,否则退回状态文案。 -->
  <span class="probe-grid">
    <el-tooltip v-for="(v, i) in grid" :key="i" :content="tooltip(i, v)" placement="top">
      <span class="probe-cell" :class="cellClass(v)" />
    </el-tooltip>
  </span>
</template>

<script setup lang="ts">
const props = defineProps<{
  grid: number[]
  // 与 grid 同序的每格计数(t=探测次数, o=成功次数);缺省退回状态文案
  stats?: { t: number; o: number }[]
}>()

// grid[23] 是当前小时,左起第 i 格是 23-i 小时前
const tooltip = (i: number, v: number): string => {
  const ago = 23 - i
  const when = ago === 0 ? '当前小时' : `${ago} 小时前`
  const st = props.stats?.[i]
  if (st && st.t > 0) return `${when} · 探测 ${st.t} 次，成功 ${st.o} 次`
  const label = v === 1 ? '全通' : v === 3 ? '全断' : v === 2 ? '部分通' : '无数据'
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
  /* 纯矩形小格(用户明确要的形态),5×10px;状态条惯例,无圆角 */
  width: 5px;
  height: 10px;
  display: inline-block;
  /* 行 hover 底色与空态灰接近,加描边保证空点可见 */
  border: 1px solid var(--ph-border);
  box-sizing: border-box;
}
.cell-ok {
  background: var(--ph-success);
  border-color: var(--ph-success);
}
.cell-down {
  background: var(--ph-danger);
  border-color: var(--ph-danger);
}
.cell-mixed {
  background: var(--ph-warning);
  border-color: var(--ph-warning);
}
.cell-none {
  background: transparent;
}
</style>
