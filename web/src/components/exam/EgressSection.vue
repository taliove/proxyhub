<template>
  <!-- 出网信息段:IPv4 / IPv6 / DNS 三行从打开即全占位;数据到达即填值,正在探测的行高亮。 -->
  <section class="exam-section">
    <header class="exam-section-head">
      <span class="exam-section-title">出网信息</span>
      <span class="exam-section-count">{{ settledCount }}/{{ rows.length }}</span>
    </header>

    <div class="exam-egress-list">
      <div
        v-for="r in rows"
        :key="r.kind"
        class="exam-row exam-egress-row"
        :class="`is-${r.status}`"
      >
        <span class="exam-egress-label">{{ r.label }}</span>

        <!-- 未到达:占位文案 -->
        <span v-if="r.status === 'waiting' || r.status === 'active'" class="exam-egress-pending">{{
          pendingText(r.status)
        }}</span>

        <!-- IPv4 出口 -->
        <template v-else-if="r.kind === 'ipv4' && egress?.ipv4">
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
        </template>

        <!-- IPv6 出口 -->
        <template v-else-if="r.kind === 'ipv6' && egress?.ipv6">
          <span class="exam-egress-value" :class="`exam-egress-${ipv6Tone(egress.ipv6)}`">{{
            ipv6Text(egress.ipv6)
          }}</span>
        </template>

        <!-- 出口 DNS -->
        <template v-else-if="r.kind === 'dns' && egress?.dns">
          <span class="exam-egress-value" :class="{ 'exam-egress-error': egress.dns.error }">{{
            dnsText(egress.dns)
          }}</span>
          <span class="exam-egress-badges">
            <span v-if="dnsLeakBadge(egress.dns)" class="exam-egress-badge exam-egress-badge-error"
              >疑似 DNS 泄露</span
            >
          </span>
        </template>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExamEgressMetrics } from '@/types'
import { buildEgressRows, type EgressRow, type RowStatus } from './examrows'
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
    active?: boolean
    terminal?: boolean
  }>(),
  { active: false, terminal: false }
)

const rows = computed<EgressRow[]>(() => buildEgressRows(props.egress, props.active))
const settledCount = computed(
  () => rows.value.filter((r) => r.status === 'ok' || r.status === 'error').length
)

const pendingText = (status: RowStatus): string =>
  status === 'active' ? '探测中' : props.terminal ? '—' : '等待'
</script>

<style scoped>
.exam-section {
  padding: var(--ph-space-2) 0;
}
.exam-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: var(--ph-space-2);
}
.exam-section-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
}
.exam-section-count {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  font-variant-numeric: tabular-nums;
}
.exam-egress-list {
  display: flex;
  flex-direction: column;
}
.exam-egress-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-1) var(--ph-space-2);
  border-bottom: 1px solid var(--ph-border-light);
  font-size: var(--ph-text-sm);
}
.exam-egress-label {
  flex: 0 0 72px;
  color: var(--ph-text-secondary);
}
.exam-egress-pending {
  font-size: var(--ph-text-xs);
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
/* 未到达行压暗;正在探测行脉冲高亮。 */
.exam-row.is-waiting {
  opacity: 0.5;
}
.exam-row.is-active {
  animation: exam-pulse 1.2s ease-in-out infinite;
}
@keyframes exam-pulse {
  0%,
  100% {
    background: transparent;
  }
  50% {
    background: color-mix(in srgb, var(--ph-color-primary) 12%, transparent);
  }
}
</style>
