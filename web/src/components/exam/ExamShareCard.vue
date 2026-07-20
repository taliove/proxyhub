<template>
  <!-- 分享卡海报:专为分享设计的独立版式(非对话框复刻)。纯展示、零按钮(按钮会被截进图),
       控件由外层 ExamShareDialog 承载。所有字段经 sharecard.ts 纯函数派生,只呈现可公开信息:
       打码节点名 + 时间 + 评分环 + 基准下行 + 多地域最佳/最差 + 解锁 6 宫格 + 出口地区/DNS + 品牌角标。
       安全:绝不渲染 UUID / 服务器地址 / 出口 IP。 -->
  <div class="share-card">
    <header class="share-head">
      <div class="share-head-main">
        <div class="share-node">{{ nodeLabel }}</div>
        <div class="share-time">{{ timeLabel }}</div>
      </div>
      <div class="share-brand-tag">ProxyHub</div>
    </header>

    <!-- 主视觉:稳定性评分环 + 基准下行 -->
    <section class="share-hero">
      <div class="share-ring-wrap">
        <svg class="share-ring" viewBox="0 0 120 120" aria-hidden="true">
          <circle class="share-ring-track" cx="60" cy="60" r="52" />
          <circle
            v-if="score !== null"
            class="share-ring-arc"
            cx="60"
            cy="60"
            r="52"
            :stroke="scoreColor"
            :stroke-dasharray="RING_CIRC"
            :stroke-dashoffset="ringOffset"
          />
        </svg>
        <div class="share-ring-center">
          <div class="share-ring-score" :style="{ color: scoreColor }">
            {{ score === null ? '—' : score }}
          </div>
          <div class="share-ring-label">{{ scoreText }}</div>
        </div>
      </div>

      <div class="share-hero-stat">
        <div class="share-stat-label">基准下行</div>
        <div class="share-stat-value">{{ baselineText }}</div>
        <div class="share-stat-sub">Cloudflare 最近节点</div>
      </div>
    </section>

    <!-- 多地域最佳/最差 -->
    <section class="share-block">
      <div class="share-block-title">多地域测速</div>
      <div class="share-extreme">
        <div class="share-extreme-row">
          <span class="share-extreme-tag share-extreme-best">最佳</span>
          <span class="share-extreme-name">{{ best ? best.name : '—' }}</span>
          <span class="share-extreme-val">{{ best ? mbps(best.down_mbps) : '—' }}</span>
        </div>
        <div class="share-extreme-row">
          <span class="share-extreme-tag share-extreme-worst">最差</span>
          <span class="share-extreme-name">{{ worst ? worst.name : '—' }}</span>
          <span class="share-extreme-val">{{ worst ? mbps(worst.down_mbps) : '—' }}</span>
        </div>
      </div>
    </section>

    <!-- 解锁 6 宫格(三档色) -->
    <section class="share-block">
      <div class="share-block-title">流媒体 / AI 解锁</div>
      <div class="share-unlock-grid">
        <div v-for="cell in unlockCells" :key="cell.name" class="share-unlock-cell">
          <span class="share-unlock-dot" :style="{ background: cellColor(cell.level) }" />
          <span class="share-unlock-name">{{ cell.name }}</span>
          <span class="share-unlock-level" :style="{ color: cellColor(cell.level) }">{{
            cell.label
          }}</span>
        </div>
      </div>
    </section>

    <!-- 出口地区 + DNS 泄露 -->
    <section class="share-block">
      <div class="share-egress">
        <div class="share-egress-item">
          <span class="share-egress-label">出口地区</span>
          <span class="share-egress-value">{{ egress.ipv4Region || '—' }}</span>
        </div>
        <div class="share-egress-item">
          <span class="share-egress-label">DNS</span>
          <span class="share-egress-value" :style="{ color: leakColor }">
            <span class="share-egress-dot" :style="{ background: leakColor }" />{{ leakText }}
          </span>
        </div>
      </div>
    </section>

    <footer class="share-foot">
      <span class="share-foot-brand">ProxyHub</span>
      <span class="share-foot-sub">深度体检报告</span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamReport } from '@/types'
import { scoreColorVar, scoreLabel } from './stability'
import {
  displayNodeName,
  formatExamTime,
  shareScore,
  shareBaselineMbps,
  shareRegionExtremes,
  shareUnlockCells,
  shareEgressSummary,
  unlockLevelColorVar,
  leakColorVar,
  leakLabel,
  type UnlockCell
} from './sharecard'
import type { UnlockLevel } from './unlock'

const props = withDefaults(
  defineProps<{
    report: ExamReport
    nodeName: string
    examTime: string | number | Date
    masked?: boolean
  }>(),
  { masked: true }
)

// 评分环几何:r=52 的周长,进度按 score/100 收拢弧长。
const RING_CIRC = 2 * Math.PI * 52

// 令牌变量取 CSS 实际值(随亮/暗主题),缺失兜底;与既有段组件同款。
const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

const nodeLabel = computed(() => displayNodeName(props.nodeName, props.masked))
const timeLabel = computed(() => formatExamTime(props.examTime))

const score = computed(() => shareScore(props.report))
const scoreColor = computed(() =>
  score.value === null
    ? cssVar('--ph-text-secondary') || '#64748b'
    : cssVar(scoreColorVar(score.value)) || '#059669'
)
const scoreText = computed(() => (score.value === null ? '未评分' : scoreLabel(score.value)))
const ringOffset = computed(() =>
  score.value === null ? RING_CIRC : RING_CIRC * (1 - Math.max(0, Math.min(100, score.value)) / 100)
)

const baseline = computed(() => shareBaselineMbps(props.report))
const baselineText = computed(() =>
  baseline.value === null ? '—' : `${baseline.value.toFixed(1)} Mbps`
)

const extremes = computed(() => shareRegionExtremes(props.report))
const best = computed(() => extremes.value.best)
const worst = computed(() => extremes.value.worst)
const mbps = (v: number) => `${v.toFixed(1)} Mbps`

const unlockCells = computed<UnlockCell[]>(() => shareUnlockCells(props.report))
const cellColor = (level: UnlockLevel) => cssVar(unlockLevelColorVar(level)) || '#64748b'

const egress = computed(() => shareEgressSummary(props.report))
const leakColor = computed(() => cssVar(leakColorVar(egress.value.dnsLeak)) || '#64748b')
const leakText = computed(() => leakLabel(egress.value.dnsLeak))
</script>

<style scoped>
.share-card {
  width: 400px;
  box-sizing: border-box;
  padding: var(--ph-space-5);
  background: var(--ph-bg-surface);
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-xl);
  color: var(--ph-text-primary);
  font-variant-numeric: tabular-nums;
}
.share-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--ph-space-3);
  margin-bottom: var(--ph-space-4);
}
.share-node {
  font-size: var(--ph-text-lg);
  font-weight: 700;
  word-break: break-all;
}
.share-time {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.share-brand-tag {
  flex: none;
  padding: 2px var(--ph-space-2);
  border-radius: var(--ph-radius-full);
  background: var(--ph-color-primary);
  color: #fff;
  font-size: var(--ph-text-xs);
  font-weight: 600;
}
.share-hero {
  display: flex;
  align-items: center;
  gap: var(--ph-space-5);
  padding: var(--ph-space-3) 0 var(--ph-space-4);
}
.share-ring-wrap {
  position: relative;
  width: 108px;
  height: 108px;
  flex: none;
}
.share-ring {
  width: 108px;
  height: 108px;
  transform: rotate(-90deg);
}
.share-ring-track {
  fill: none;
  stroke: var(--ph-border);
  stroke-width: 10;
}
.share-ring-arc {
  fill: none;
  stroke-width: 10;
  stroke-linecap: round;
  transition: stroke-dashoffset 0.4s ease;
}
.share-ring-center {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}
.share-ring-score {
  font-size: var(--ph-text-display);
  font-weight: 700;
  line-height: 1;
}
.share-ring-label {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.share-hero-stat {
  flex: 1;
  min-width: 0;
}
.share-stat-label {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.share-stat-value {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-2xl);
  font-weight: 700;
}
.share-stat-sub {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.share-block {
  padding: var(--ph-space-3) 0;
  border-top: 1px solid var(--ph-border-light);
}
.share-block-title {
  margin-bottom: var(--ph-space-2);
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-regular);
}
.share-extreme {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-2);
}
.share-extreme-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  font-size: var(--ph-text-sm);
}
.share-extreme-tag {
  flex: none;
  padding: 0 var(--ph-space-2);
  border-radius: var(--ph-radius-full);
  font-size: var(--ph-text-xs);
  font-weight: 600;
}
.share-extreme-best {
  color: var(--ph-success);
  background: color-mix(in srgb, var(--ph-success) 14%, transparent);
}
.share-extreme-worst {
  color: var(--ph-warning);
  background: color-mix(in srgb, var(--ph-warning) 14%, transparent);
}
.share-extreme-name {
  flex: 1;
  font-weight: 500;
}
.share-extreme-val {
  font-weight: 600;
}
.share-unlock-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--ph-space-2) var(--ph-space-4);
}
.share-unlock-cell {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  font-size: var(--ph-text-sm);
}
.share-unlock-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: var(--ph-radius-full);
}
.share-unlock-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ph-text-regular);
}
.share-unlock-level {
  flex: none;
  font-weight: 600;
}
.share-egress {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-2);
}
.share-egress-item {
  display: flex;
  align-items: baseline;
  gap: var(--ph-space-3);
  font-size: var(--ph-text-sm);
}
.share-egress-label {
  flex: none;
  width: 64px;
  color: var(--ph-text-secondary);
}
.share-egress-value {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  font-weight: 500;
  word-break: break-all;
}
.share-egress-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: var(--ph-radius-full);
}
.share-foot {
  display: flex;
  align-items: baseline;
  gap: var(--ph-space-2);
  margin-top: var(--ph-space-4);
  padding-top: var(--ph-space-3);
  border-top: 1px solid var(--ph-border-light);
}
.share-foot-brand {
  font-size: var(--ph-text-sm);
  font-weight: 700;
  color: var(--ph-color-primary);
}
.share-foot-sub {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
</style>
