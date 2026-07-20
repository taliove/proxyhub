// 出网信息段(体检第三段,与解锁并行)的纯计算逻辑(与渲染解耦,便于单测)。
// IPv4 出口 / IPv6 出口 / 出口 DNS 三类,随 SSE 逐条到达,段末给聚合(含 DNS 泄露判定)。
import type { ExamEgressMetrics, ExamEgressIPv4, ExamEgressIPv6, ExamEgressDNS } from '@/types'

// mergeEgress 以子项为粒度不可变叠加一份出网结果:incoming 中非空的子项覆盖 prev 对应子项。
// 逐条 egress 帧各带一类子项,段末 section_done 带全量(权威,含泄露标记)。
export function mergeEgress(
  prev: ExamEgressMetrics | null,
  incoming: ExamEgressMetrics
): ExamEgressMetrics {
  const base = prev ?? {}
  return {
    ipv4: incoming.ipv4 ?? base.ipv4,
    ipv6: incoming.ipv6 ?? base.ipv6,
    dns: incoming.dns ?? base.dns
  }
}

function joinDot(parts: (string | undefined)[]): string {
  return parts
    .map((p) => (p ?? '').trim())
    .filter((p) => p !== '')
    .join(' · ')
}

// ipv4Location 出口地区:国家 · 省 · 市(跳过空段)。
export function ipv4Location(v: ExamEgressIPv4): string {
  return joinDot([v.country, v.region, v.city])
}

// ipv4Asn 出口 ASN 与运营商/机房:"AS64500 Example Hosting"(任一缺失则只取另一)。
export function ipv4Asn(v: ExamEgressIPv4): string {
  return [v.asn, v.org]
    .map((p) => (p ?? '').trim())
    .filter((p) => p !== '')
    .join(' ')
}

// 徽标语义色调:ok 绿 / warn 黄 / muted 中性 / error 红。
export type EgressTone = 'ok' | 'warn' | 'muted' | 'error'

// hostingBadge 机房/住宅标记:hosting 为机房(warn),否则住宅(ok);探测失败(有 error)不给标记。
export function hostingBadge(v: ExamEgressIPv4): { text: string; tone: EgressTone } | null {
  if (v.error) return null
  return v.hosting ? { text: '机房', tone: 'warn' } : { text: '住宅', tone: 'ok' }
}

// proxyBadge 是否命中疑似代理/VPN 出口(命中则渲染"代理"警示徽标)。
export function proxyBadge(v: ExamEgressIPv4): boolean {
  return v.proxy === true
}

// ipv6Text IPv6 出口三态文案:有出口给地址;不可达"无 IPv6 出口";解析失败"探测异常"(与不可达区分)。
export function ipv6Text(v: ExamEgressIPv6): string {
  if (v.available) return v.address ?? '有 IPv6 出口'
  return v.error ? '探测异常' : '无 IPv6 出口'
}

// ipv6Tone IPv6 三态色调:有出口 ok / 不可达 muted / 探测异常 error。
export function ipv6Tone(v: ExamEgressIPv6): EgressTone {
  if (v.available) return 'ok'
  return v.error ? 'error' : 'muted'
}

// dnsText 出口 DNS 文案:解析器 IP · 归属地;探测失败给"探测异常"。
export function dnsText(v: ExamEgressDNS): string {
  if (v.error) return '探测异常'
  return joinDot([v.resolver_ip, v.resolver_geo])
}

// dnsLeakBadge 是否疑似 DNS 泄露(解析器国家与出口国家不一致)。
export function dnsLeakBadge(v: ExamEgressDNS): boolean {
  return v.leak === true
}
