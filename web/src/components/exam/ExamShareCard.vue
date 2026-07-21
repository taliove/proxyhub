<template>
  <!-- 分享卡海报:专为分享设计的独立版式。纯展示、零按钮,控件由外层 ExamShareDialog 承载。
       字段经 sharecard.ts 派生:打码节点名、时间、评分环、基准速率、地域极值、解锁 6 宫格、
       出口地区 + 可选出口/入口 IP + DNS 泄露(+ 可选解析器)。
       安全:默认不渲染任何 IP/服务器地址,仅当对应 showXxx=true 时显示。 -->
  <div class="share-card">
    <header class="share-head">
      <div class="share-head-main">
        <div class="share-node">{{ nodeLabel }}</div>
        <div class="share-time">{{ timeLabel }}</div>
      </div>
      <div class="share-brand-tag">ProxyHub</div>
    </header>
    <!-- 主视觉:稳定性评分环 + 基准速率 -->
    <section class="share-hero">
      <div class="share-ring-wrap">
        <svg class="share-ring" viewBox="0 0 120 120" aria-hidden="true">
          <circle class="share-ring-track" cx="60" cy="60" r="52" />
          <circle
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
            {{ Math.round(score) }}
          </div>
          <div class="share-ring-label">{{ scoreText }}</div>
        </div>
      </div>
      <div class="share-hero-stat">
        <div class="share-stat-label">基准下行</div>
        <div class="share-stat-value">{{ baselineDownText }}</div>
        <div v-if="baselineUpText" class="share-stat-sub-up">上行 {{ baselineUpText }}</div>
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
          <span class="share-extreme-val">
            {{ best ? mbps(best.down_mbps) : '—' }}
            <span v-if="best" class="share-extreme-latency">{{ ms(best.ttfb_ms) }}</span>
          </span>
        </div>
        <div class="share-extreme-row">
          <span class="share-extreme-tag share-extreme-worst">最差</span>
          <span class="share-extreme-name">{{ worst ? worst.name : '—' }}</span>
          <span class="share-extreme-val">
            {{ worst ? mbps(worst.down_mbps) : '—' }}
            <span v-if="worst" class="share-extreme-latency">{{ ms(worst.ttfb_ms) }}</span>
          </span>
        </div>
      </div>
    </section>
    <!-- 解锁 6 宫格 -->
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
    <!-- 出口地区 + 可选出口/入口 IP + DNS 泄露(+ 可选解析器) -->
    <section class="share-block">
      <div class="share-egress">
        <div class="share-egress-item">
          <span class="share-egress-label">出口地区</span>
          <span class="share-egress-value">{{ egress.ipv4Region || '—' }}</span>
        </div>
        <div v-if="egress.egressIp" class="share-egress-item">
          <span class="share-egress-label">出口 IP</span>
          <span class="share-egress-value">{{ egress.egressIp }}</span>
        </div>
        <div v-if="egress.ingressIp" class="share-egress-item">
          <span class="share-egress-label">入口 IP</span>
          <span class="share-egress-value">{{ egress.ingressIp }}</span>
        </div>
        <div class="share-egress-item">
          <span class="share-egress-label">DNS</span>
          <span class="share-egress-value" :style="{ color: leakColor }">
            <span class="share-egress-dot" :style="{ background: leakColor }" />{{ leakText }}
          </span>
        </div>
        <div v-if="egress.dnsResolver" class="share-egress-item">
          <span class="share-egress-label">解析器</span>
          <span class="share-egress-value">{{ egress.dnsResolver }}</span>
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
import { gradeColorVar, gradeLabel } from './score'
import {
  displayNodeName,
  formatExamTime,
  shareOverallScore,
  shareBaselineMbps,
  shareBaselineUpMbps,
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
    nodeServer?: string
    masked?: boolean
    showEgressIp?: boolean
    showIngressIp?: boolean
    showDns?: boolean
  }>(),
  { masked: true, nodeServer: '', showEgressIp: false, showIngressIp: false, showDns: false }
)
// 评分环几何:r=52 的周长,进度按 score/100 收拢弧长。
const RING_CIRC = 2 * Math.PI * 52
// 令牌变量取 CSS 实际值(随亮/暗主题),缺失兜底;与既有段组件同款。
const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()
const nodeLabel = computed(() => displayNodeName(props.nodeName, props.masked))
const timeLabel = computed(() => formatExamTime(props.examTime))
const overallScore = computed(() => shareOverallScore(props.report))
const score = computed(() => overallScore.value.total)
const scoreColor = computed(() => cssVar(gradeColorVar(overallScore.value.grade)) || '#059669')
const scoreText = computed(() => gradeLabel(overallScore.value.grade))
const ringOffset = computed(() => RING_CIRC * (1 - Math.max(0, Math.min(100, score.value)) / 100))
const baseline = computed(() => shareBaselineMbps(props.report))
const baselineDownText = computed(() =>
  baseline.value === null ? '—' : `${baseline.value.toFixed(1)} Mbps`
)
const baselineUp = computed(() => shareBaselineUpMbps(props.report))
const baselineUpText = computed(() =>
  baselineUp.value === null ? null : `${baselineUp.value.toFixed(1)} Mbps`
)
const extremes = computed(() => shareRegionExtremes(props.report))
const best = computed(() => extremes.value.best)
const worst = computed(() => extremes.value.worst)
const mbps = (v: number) => `${v.toFixed(1)} Mbps`
const ms = (v: number) => `${Math.round(v)} ms`
const unlockCells = computed<UnlockCell[]>(() => shareUnlockCells(props.report))
const cellColor = (level: UnlockLevel) => cssVar(unlockLevelColorVar(level)) || '#64748b'
const egress = computed(() =>
  shareEgressSummary(props.report, {
    showEgressIp: props.showEgressIp,
    showIngressIp: props.showIngressIp,
    showDns: props.showDns,
    ingressIp: props.nodeServer
  })
)
const leakColor = computed(() => cssVar(leakColorVar(egress.value.dnsLeak)) || '#64748b')
const leakText = computed(() => leakLabel(egress.value.dnsLeak))
</script>
<style scoped src="./ExamShareCard.css"></style>
