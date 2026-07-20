<template>
  <!-- 出网信息区(体检第三段,与解锁并行):IPv4 / IPv6 / DNS 三条,随 SSE 逐条到达;历史报告一次性给全量。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">出网信息</span>
      <span v-if="subText" class="exam-section-sub">{{ subText }}</span>
    </header>

    <div v-if="hasAny" class="exam-egress-list">
      <!-- IPv4 出口 -->
      <div v-if="egress?.ipv4" class="exam-egress-row">
        <span class="exam-egress-label">IPv4 出口</span>
        <template v-if="egress.ipv4.error">
          <span class="exam-egress-value exam-egress-error">探测失败</span>
        </template>
        <template v-else>
          <span class="exam-egress-value">
            <span class="exam-egress-ip">{{ egress.ipv4.ip || '-' }}</span>
            <span v-if="ipv4Location(egress.ipv4)" class="exam-egress-sub">{{
              ipv4Location(egress.ipv4)
            }}</span>
            <span v-if="ipv4Asn(egress.ipv4)" class="exam-egress-sub">{{
              ipv4Asn(egress.ipv4)
            }}</span>
          </span>
          <span class="exam-egress-badges">
            <span
              v-if="hostingBadge(egress.ipv4)"
              class="exam-egress-badge"
              :class="`exam-egress-badge-${hostingBadge(egress.ipv4)!.tone}`"
              >{{ hostingBadge(egress.ipv4)!.text }}</span
            >
            <span v-if="proxyBadge(egress.ipv4)" class="exam-egress-badge exam-egress-badge-error"
              >代理</span
            >
          </span>
        </template>
      </div>

      <!-- IPv6 出口 -->
      <div v-if="egress?.ipv6" class="exam-egress-row">
        <span class="exam-egress-label">IPv6 出口</span>
        <span class="exam-egress-value" :class="`exam-egress-${ipv6Tone(egress.ipv6)}`">{{
          ipv6Text(egress.ipv6)
        }}</span>
      </div>

      <!-- 出口 DNS -->
      <div v-if="egress?.dns" class="exam-egress-row">
        <span class="exam-egress-label">出口 DNS</span>
        <span class="exam-egress-value" :class="{ 'exam-egress-error': egress.dns.error }">{{
          dnsText(egress.dns)
        }}</span>
        <span class="exam-egress-badges">
          <span v-if="dnsLeakBadge(egress.dns)" class="exam-egress-badge exam-egress-badge-error"
            >疑似 DNS 泄露</span
          >
        </span>
      </div>
    </div>
    <div v-else class="exam-egress-empty">{{ emptyText }}</div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamEgressMetrics } from '@/types'
import {
  ipv4Location,
  ipv4Asn,
  hostingBadge,
  proxyBadge,
  ipv6Text,
  ipv6Tone,
  dnsText,
  dnsLeakBadge
} from './egress'

const props = withDefaults(
  defineProps<{
    egress: ExamEgressMetrics | null
    subText?: string
    emptyText?: string
  }>(),
  { subText: '', emptyText: '等待出网信息…' }
)

const hasAny = computed(
  () => !!(props.egress && (props.egress.ipv4 || props.egress.ipv6 || props.egress.dns))
)
</script>

<style scoped>
.exam-section {
  padding: var(--ph-space-2) 0;
}
.exam-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: var(--ph-space-3);
}
.exam-section-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
}
.exam-section-sub {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.exam-egress-list {
  display: flex;
  flex-direction: column;
}
.exam-egress-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-2) 0;
  border-bottom: 1px solid var(--ph-border-light);
  font-size: var(--ph-text-sm);
}
.exam-egress-label {
  flex: 0 0 72px;
  color: var(--ph-text-secondary);
}
.exam-egress-value {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--ph-space-1) var(--ph-space-2);
  flex: 1 1 auto;
  font-variant-numeric: tabular-nums;
}
.exam-egress-ip {
  font-weight: 600;
}
.exam-egress-sub {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-egress-ok {
  color: var(--ph-success);
}
.exam-egress-muted {
  color: var(--ph-text-secondary);
}
.exam-egress-error {
  color: var(--ph-danger);
}
.exam-egress-badges {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.exam-egress-badge {
  padding: 0 var(--ph-space-2);
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius-full);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-egress-badge-ok {
  color: var(--ph-success);
  border-color: var(--ph-success);
}
.exam-egress-badge-warn {
  color: var(--ph-warning);
  border-color: var(--ph-warning);
}
.exam-egress-badge-error {
  color: var(--ph-danger);
  border-color: var(--ph-danger);
}
.exam-egress-empty {
  padding: var(--ph-space-4);
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
</style>
