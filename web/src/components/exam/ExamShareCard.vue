<template>
  <!-- 分享卡海报:专为分享设计的独立版式。纯展示、零按钮,控件由外层 ExamShareDialog 承载。
       两态:showAll=false(默认)显示脱敏摘要(打码节点名、无 IP、最佳/最差、稳定性明细);
       showAll=true 显示全量版(完整节点名、全 IP、多地域全行表格、稳定性明细、出网全字段)。 -->
  <div class="share-card" :class="{ 'share-card-full': showAll }">
    <header class="share-head">
      <div class="share-head-main">
        <div class="share-node">{{ vm.nodeLabel }}</div>
        <div class="share-time">{{ vm.timeLabel }}</div>
      </div>
      <div class="share-brand-tag">ProxyHub</div>
    </header>
    <!-- 主视觉:稳定性评分环 + 基准速率 -->
    <section class="share-hero">
      <div class="share-ring-wrap">
        <svg class="share-ring" viewBox="0 0 120 120" aria-hidden="true">
          <!-- 关键视觉属性(fill/stroke/stroke-width)以 presentation attributes 内联,
               不依赖样式表:html-to-image 导出时外部 scoped CSS 应用不全,且缺 fill 的
               circle 会默认黑填充。内联后导出 PNG 与页面一致(环心透明、不发黑)。 -->
          <circle
            class="share-ring-track"
            cx="60"
            cy="60"
            r="52"
            fill="none"
            :stroke="trackColor"
            stroke-width="10"
          />
          <circle
            class="share-ring-arc"
            cx="60"
            cy="60"
            r="52"
            fill="none"
            :stroke="scoreColor"
            stroke-width="10"
            stroke-linecap="round"
            :stroke-dasharray="RING_CIRC"
            :stroke-dashoffset="ringOffset"
          />
        </svg>
        <div class="share-ring-center">
          <div class="share-ring-score" :style="{ color: scoreColor }">
            {{ Math.round(vm.score.total) }}
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
    <!-- 多地域:摘要版显示最佳/最差,全量版显示完整表格 -->
    <section class="share-block">
      <div class="share-block-title">多地域测速</div>
      <!-- 摘要版:最佳/最差两行 -->
      <div v-if="!showAll" class="share-extreme">
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
      <!-- 全量版:完整表格 -->
      <div v-else class="share-region-table">
        <div class="share-region-header">
          <span class="share-region-col-name">区域</span>
          <span class="share-region-col-latency">延迟</span>
          <span class="share-region-col-down">下行</span>
          <span class="share-region-col-up">上行</span>
        </div>
        <div v-for="region in vm.allRegions" :key="region.code" class="share-region-row">
          <span class="share-region-col-name">{{ region.name }}</span>
          <span class="share-region-col-latency">{{ ms(region.ttfb_ms) }}</span>
          <span class="share-region-col-down">{{ mbps(region.down_mbps) }}</span>
          <span class="share-region-col-up">{{
            region.up_mbps !== undefined ? mbps(region.up_mbps) : '—'
          }}</span>
        </div>
      </div>
    </section>
    <!-- 稳定性明细:默认摘要版与全量版均显示(不含敏感信息) -->
    <section v-if="vm.stabilityDetails" class="share-block">
      <div class="share-block-title">稳定性指标</div>
      <div class="share-stability-grid">
        <div class="share-stability-item">
          <span class="share-stability-label">丢包率</span>
          <span class="share-stability-value">{{ pct(vm.stabilityDetails.loss_rate) }}</span>
        </div>
        <div class="share-stability-item">
          <span class="share-stability-label">平均延迟</span>
          <span class="share-stability-value">{{ ms(vm.stabilityDetails.mean_ms) }}</span>
        </div>
        <div class="share-stability-item">
          <span class="share-stability-label">中位延迟</span>
          <span class="share-stability-value">{{ ms(vm.stabilityDetails.median_ms) }}</span>
        </div>
        <div class="share-stability-item">
          <span class="share-stability-label">P95</span>
          <span class="share-stability-value">{{ ms(vm.stabilityDetails.p95_ms) }}</span>
        </div>
        <div class="share-stability-item">
          <span class="share-stability-label">P99</span>
          <span class="share-stability-value">{{ ms(vm.stabilityDetails.p99_ms) }}</span>
        </div>
        <div class="share-stability-item">
          <span class="share-stability-label">抖动</span>
          <span class="share-stability-value">{{ ms(vm.stabilityDetails.jitter_ms) }}</span>
        </div>
      </div>
    </section>
    <!-- 解锁 6 宫格 -->
    <section class="share-block">
      <div class="share-block-title">流媒体 / AI 解锁</div>
      <div class="share-unlock-grid">
        <div v-for="cell in vm.unlockCells" :key="cell.name" class="share-unlock-cell">
          <span class="share-unlock-dot" :style="{ background: cellColor(cell.level) }" />
          <span class="share-unlock-name">{{ cell.name }}</span>
          <span class="share-unlock-level" :style="{ color: cellColor(cell.level) }">{{
            cell.label
          }}</span>
        </div>
      </div>
    </section>
    <!-- 出口地区 + 可选 IP + DNS 泄露 -->
    <section class="share-block">
      <div class="share-egress">
        <div class="share-egress-item">
          <span class="share-egress-label">出口地区</span>
          <span class="share-egress-value">{{ vm.egress.ipv4Region || '—' }}</span>
        </div>
        <div v-if="vm.egress.egressIp" class="share-egress-item">
          <span class="share-egress-label">出口 IP</span>
          <span class="share-egress-value">{{ vm.egress.egressIp }}</span>
        </div>
        <div v-if="vm.egress.ingressIp" class="share-egress-item">
          <span class="share-egress-label">入口 IP</span>
          <span class="share-egress-value">{{ vm.egress.ingressIp }}</span>
        </div>
        <div class="share-egress-item">
          <span class="share-egress-label">DNS</span>
          <span class="share-egress-value" :style="{ color: leakColor }">
            <span class="share-egress-dot" :style="{ background: leakColor }" />{{ leakText }}
          </span>
        </div>
        <div v-if="vm.egress.dnsResolver" class="share-egress-item">
          <span class="share-egress-label">解析器</span>
          <span class="share-egress-value">{{ vm.egress.dnsResolver }}</span>
        </div>
        <!-- 全量版:额外出网字段 -->
        <template v-if="showAll">
          <div v-if="vm.egress.asn" class="share-egress-item">
            <span class="share-egress-label">ASN</span>
            <span class="share-egress-value">{{ vm.egress.asn }}</span>
          </div>
          <div v-if="vm.egress.org" class="share-egress-item">
            <span class="share-egress-label">组织</span>
            <span class="share-egress-value">{{ vm.egress.org }}</span>
          </div>
          <div v-if="vm.egress.proxy !== undefined" class="share-egress-item">
            <span class="share-egress-label">代理</span>
            <span class="share-egress-value">{{ vm.egress.proxy ? '是' : '否' }}</span>
          </div>
          <div v-if="vm.egress.hosting !== undefined" class="share-egress-item">
            <span class="share-egress-label">机房</span>
            <span class="share-egress-value">{{ vm.egress.hosting ? '是' : '否' }}</span>
          </div>
        </template>
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
import { shareViewModel, unlockLevelColorVar, leakColorVar, leakLabel } from './sharecard'
import type { UnlockLevel } from './unlock'

const props = withDefaults(
  defineProps<{
    report: ExamReport
    nodeName: string
    examTime: string | number | Date
    nodeServer?: string
    showAll?: boolean
  }>(),
  { nodeServer: '', showAll: false }
)
// 评分环几何:r=52 的周长,进度按 score/100 收拢弧长。
const RING_CIRC = 2 * Math.PI * 52
// 令牌变量取 CSS 实际值(随亮/暗主题),缺失兜底;与既有段组件同款。
const cssVar = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim()

const vm = computed(() =>
  shareViewModel(props.report, {
    showAll: props.showAll,
    nodeName: props.nodeName,
    examTime: props.examTime,
    ingressIp: props.nodeServer
  })
)

const scoreColor = computed(() => cssVar(gradeColorVar(vm.value.score.grade)) || '#059669')
// 环底轨道色:取边框令牌(随主题),缺失兜底。与弧色一样内联到 SVG,避免导出时丢样式。
const trackColor = computed(() => cssVar('--ph-border') || '#e2e8f0')
const scoreText = computed(() => gradeLabel(vm.value.score.grade))
const ringOffset = computed(
  () => RING_CIRC * (1 - Math.max(0, Math.min(100, vm.value.score.total)) / 100)
)
const baselineDownText = computed(() =>
  vm.value.baselineDown === null ? '—' : `${vm.value.baselineDown.toFixed(1)} Mbps`
)
const baselineUpText = computed(() =>
  vm.value.baselineUp === null ? null : `${vm.value.baselineUp.toFixed(1)} Mbps`
)
const best = computed(() => vm.value.regionSummary.best)
const worst = computed(() => vm.value.regionSummary.worst)
const mbps = (v: number) => `${v.toFixed(1)} Mbps`
const ms = (v: number) => `${Math.round(v)} ms`
const pct = (v: number) => `${(v * 100).toFixed(1)}%`
const cellColor = (level: UnlockLevel) => cssVar(unlockLevelColorVar(level)) || '#64748b'
const leakColor = computed(() => cssVar(leakColorVar(vm.value.egress.dnsLeak)) || '#64748b')
const leakText = computed(() => leakLabel(vm.value.egress.dnsLeak))
</script>
<style scoped src="./ExamShareCard.css"></style>
