// 分享卡的纯计算逻辑(与渲染解耦,便于单测)。
// 只从 ExamReport 派生「可公开」字段:打码节点名、评分、基准下行/上行、多地域最佳/最差、
// 解锁 6 宫格、出口地区 + DNS 泄露状态。出口 IPv4 仅当显式开关时返回,默认不派生。
import type { ExamReport, ExamRegionResult, ExamUnlockResult, ExamStabilityMetrics } from '@/types'
import { isBaselineRow, EXAM_UNLOCK_SLOTS } from './examrows'
import { unlockLevel, unlockLabel, type UnlockLevel } from './unlock'
import { ipv4Location } from './egress'
import { calculateExamScore, type ExamScoreResult } from './score'

export const MASK = '***'
export const UNNAMED = '未命名节点'

// 打码分隔符:按常见节点命名切段。刻意不含 '.',避免把 IP/域名切碎后反而暴露片段。
const NAME_DELIMS = /[-_/|·\s@:]+/

// maskNodeName 把节点名打码:保留可辨识前缀,尾段(常含主机/IP/序号)以 *** 遮蔽。
// 多段(>=2):保留前 1~2 段 + '-***'(如 233boy-grpc-1.2.3.4 -> 233boy-grpc-***);
// 单段:仅保留至多 2 个字符 + '***';空名回落占位。
export function maskNodeName(name: string): string {
  const trimmed = (name ?? '').trim()
  if (trimmed === '') return UNNAMED
  const segs = trimmed.split(NAME_DELIMS).filter((s) => s !== '')
  if (segs.length >= 2) {
    const keep = Math.min(2, segs.length - 1)
    return `${segs.slice(0, keep).join('-')}-${MASK}`
  }
  const head = trimmed.slice(0, Math.max(1, Math.min(2, trimmed.length - 1)))
  return `${head}${MASK}`
}

// displayNodeName 依打码开关给出卡片展示名(打码默认开)。
export function displayNodeName(name: string, masked: boolean): string {
  if (masked) return maskNodeName(name)
  const trimmed = (name ?? '').trim()
  return trimmed === '' ? UNNAMED : trimmed
}

// formatExamTime 体检时间:本地 YYYY-MM-DD HH:mm;无效/缺失回落 '—'。
export function formatExamTime(t: string | number | Date | undefined): string {
  if (t === undefined || t === '') return '—'
  const d = t instanceof Date ? t : new Date(t)
  const ms = d.getTime()
  if (!Number.isFinite(ms)) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// shareScore 稳定性评分;无稳定性段返回 null(评分环不渲染)。
// DEPRECATED:推荐改用 shareOverallScore 获取总分,此函数仅为向后兼容保留。
export function shareScore(report: ExamReport): number | null {
  return report.stability ? report.stability.score : null
}

// shareOverallScore 体检总分(加权四项);返回完整评分结果(总分 + 档位 + 分解 + 部分标记)。
export function shareOverallScore(report: ExamReport): ExamScoreResult {
  return calculateExamScore(report)
}

// shareBaselineMbps 基准(Cloudflare 最近 POP)下行速率;无基准或失败返回 null。
export function shareBaselineMbps(report: ExamReport): number | null {
  const regions = report.region_speed?.regions ?? []
  const baseline = regions.find((r) => isBaselineRow(r))
  if (!baseline || baseline.error) return null
  return Number.isFinite(baseline.down_mbps) ? baseline.down_mbps : null
}

// shareBaselineUpMbps 基准(Cloudflare 最近 POP)上行速率;无基准/失败/后端未返回上行字段返回 null。
export function shareBaselineUpMbps(report: ExamReport): number | null {
  const regions = report.region_speed?.regions ?? []
  const baseline = regions.find((r) => isBaselineRow(r))
  if (!baseline || baseline.error) return null
  return baseline.up_mbps !== undefined && Number.isFinite(baseline.up_mbps)
    ? baseline.up_mbps
    : null
}

// RegionExtreme 多地域极值行(展示区域名 + 速率 + 延迟,无任何地址)。
export interface RegionExtreme {
  name: string
  down_mbps: number
  ttfb_ms: number
}

// shareRegionExtremes 非基准且成功的区域中:下行最高=最佳,最低=最差;不足则对应项为 null。
export function shareRegionExtremes(report: ExamReport): {
  best: RegionExtreme | null
  worst: RegionExtreme | null
} {
  const regions = (report.region_speed?.regions ?? []).filter(
    (r) => !isBaselineRow(r) && !r.error && Number.isFinite(r.down_mbps)
  )
  if (regions.length === 0) return { best: null, worst: null }
  let best = regions[0]
  let worst = regions[0]
  for (const r of regions) {
    if (r.down_mbps > best.down_mbps) best = r
    if (r.down_mbps < worst.down_mbps) worst = r
  }
  const pick = (r: ExamRegionResult): RegionExtreme => ({
    name: r.name,
    down_mbps: r.down_mbps,
    ttfb_ms: r.ttfb_ms
  })
  return { best: pick(best), worst: pick(worst) }
}

// UnlockCell 解锁 6 宫格单元(名称 + 三档 level + 中文标签)。
export interface UnlockCell {
  name: string
  level: UnlockLevel
  label: string
}

// shareUnlockCells 6 宫格解锁:按固定槽位归位;缺失项为 unknown/'未测'。
export function shareUnlockCells(report: ExamReport): UnlockCell[] {
  const byName = new Map<string, ExamUnlockResult>()
  for (const r of report.unlock?.results ?? []) byName.set(r.target_name, r)
  return EXAM_UNLOCK_SLOTS.map((name) => {
    const r = byName.get(name)
    if (!r) return { name, level: 'unknown', label: '未测' }
    return { name, level: unlockLevel(r), label: unlockLabel(r) }
  })
}

// unlockLevelColorVar 三档解锁 level 对应设计令牌(随亮/暗主题):full 绿 / originals_only 黄 / blocked 红 / unknown 中性。
export function unlockLevelColorVar(level: UnlockLevel): string {
  switch (level) {
    case 'full':
      return '--ph-success'
    case 'originals_only':
      return '--ph-warning'
    case 'blocked':
      return '--ph-danger'
    default:
      return '--ph-text-secondary'
  }
}

// LeakStatus DNS 泄露三态:未泄露 ok / 疑似泄露 leak / 未知(缺失或探测异常)unknown。
export type LeakStatus = 'ok' | 'leak' | 'unknown'

// EgressSummary 出口摘要(地区 + 可选出口 IP + 可选入口 IP + 可选 DNS 解析器)。
// 三个 IP 字段各有独立开关,默认全隐藏(高敏信息);showXxx=true 时填充对应字段。
export interface EgressSummary {
  ipv4Region: string // 出口地区:国家·省·市;失败或缺失为空串
  egressIp?: string // 出口 IPv4 地址;仅当 showEgressIp=true 时填充
  ingressIp?: string // 入口 IP(节点 server);仅当 showIngressIp=true 时填充
  dnsResolver?: string // DNS 解析器 IP + 地区;仅当 showDns=true 时填充
  dnsLeak: LeakStatus // DNS 泄露状态(与 showDns 独立:状态码始终可见,解析器详情受开关控制)
  // 全量版额外字段(showAll=true 时填充)
  asn?: string // ASN 编号
  org?: string // 组织名称
  proxy?: boolean // 疑似代理标记
  hosting?: boolean // 机房标记
}

// shareEgressSummary 出口摘要:IPv4 出口「地区」+ 三个可选地址字段 + DNS 泄露状态 + 可选全量字段。
// 安全契约:默认不返回任何 IP/服务器地址;仅当对应 showXxx=true 时填充。
// showAll=true 时额外填充 asn/org/proxy/hosting 等全量字段(用于分享卡全量版)。
// ingressIp(节点入口地址)由调用方从外部传入(不在 report.egress 里),例如 report 宿主传入 node.server。
export function shareEgressSummary(
  report: ExamReport,
  options: {
    showEgressIp?: boolean
    showIngressIp?: boolean
    showDns?: boolean
    showAll?: boolean
    ingressIp?: string
  } = {}
): EgressSummary {
  const {
    showEgressIp = false,
    showIngressIp = false,
    showDns = false,
    showAll = false,
    ingressIp
  } = options
  const e = report.egress
  const ipv4 = e?.ipv4
  const ipv4Region = ipv4 && !ipv4.error ? ipv4Location(ipv4) : ''
  const egressIp = (showEgressIp || showAll) && ipv4 && !ipv4.error ? ipv4.ip : undefined
  const ingressIpValue = (showIngressIp || showAll) && ingressIp ? ingressIp : undefined
  const dns = e?.dns
  let dnsLeak: LeakStatus = 'unknown'
  if (dns && !dns.error) dnsLeak = dns.leak ? 'leak' : 'ok'
  const dnsResolver =
    (showDns || showAll) && dns && !dns.error
      ? `${dns.resolver_ip}${dns.resolver_geo ? ` (${dns.resolver_geo})` : ''}`
      : undefined

  const summary: EgressSummary = {
    ipv4Region,
    egressIp,
    ingressIp: ingressIpValue,
    dnsResolver,
    dnsLeak
  }

  // 全量版:额外填充 ASN/组织/标记字段
  if (showAll && ipv4 && !ipv4.error) {
    summary.asn = ipv4.asn
    summary.org = ipv4.org
    summary.proxy = ipv4.proxy
    summary.hosting = ipv4.hosting
  }

  return summary
}

// leakColorVar DNS 泄露状态色:未泄露 绿 / 疑似泄露 红 / 未知 中性。
export function leakColorVar(status: LeakStatus): string {
  switch (status) {
    case 'ok':
      return '--ph-success'
    case 'leak':
      return '--ph-danger'
    default:
      return '--ph-text-secondary'
  }
}

// leakLabel DNS 泄露状态中文标签。
export function leakLabel(status: LeakStatus): string {
  switch (status) {
    case 'ok':
      return '未泄露'
    case 'leak':
      return '疑似泄露'
    default:
      return '未知'
  }
}

// ShareViewModelOptions shareViewModel 的输入选项(统一 showAll 主开关)。
export interface ShareViewModelOptions {
  showAll?: boolean // 主开关:false(默认)=脱敏摘要版,true=全量版
  nodeName: string
  examTime?: string | number | Date
  ingressIp?: string
}

// ShareViewModel 分享卡统一视图模型(showAll 控制摘要/全量两态)。
export interface ShareViewModel {
  nodeLabel: string
  timeLabel: string
  score: ExamScoreResult
  baselineDown: number | null
  baselineUp: number | null
  regionSummary: { best: RegionExtreme | null; worst: RegionExtreme | null }
  allRegions?: ExamRegionResult[] // 仅全量版:多地域全行(除基准外)
  stabilityDetails?: ExamStabilityMetrics // 仅全量版:稳定性明细指标
  unlockCells: UnlockCell[]
  egress: EgressSummary
}

// shareViewModel 分享卡统一视图模型派生函数:单一 showAll 开关控制摘要/全量两态。
// showAll=false(默认):打码节点名、无 IP、多地域仅最佳/最差、无稳定性明细。
// showAll=true:完整节点名、全 IP/ASN、多地域全行、稳定性明细、出网全字段。
export function shareViewModel(report: ExamReport, options: ShareViewModelOptions): ShareViewModel {
  const { showAll = false, nodeName, examTime, ingressIp } = options

  const nodeLabel = displayNodeName(nodeName, !showAll)
  const timeLabel = formatExamTime(examTime)
  const score = shareOverallScore(report)
  const baselineDown = shareBaselineMbps(report)
  const baselineUp = shareBaselineUpMbps(report)
  const regionSummary = shareRegionExtremes(report)
  const unlockCells = shareUnlockCells(report)
  const egress = shareEgressSummary(report, { showAll, ingressIp })

  const vm: ShareViewModel = {
    nodeLabel,
    timeLabel,
    score,
    baselineDown,
    baselineUp,
    regionSummary,
    unlockCells,
    egress
  }

  // 全量版:额外派生多地域全行与稳定性明细
  if (showAll) {
    const regions = report.region_speed?.regions ?? []
    vm.allRegions = regions.filter((r) => !isBaselineRow(r) && !r.error)
    vm.stabilityDetails = report.stability
      ? {
          score: report.stability.score,
          total: report.stability.total,
          succeeded: report.stability.succeeded,
          loss_rate: report.stability.loss_rate,
          mean_ms: report.stability.mean_ms,
          median_ms: report.stability.median_ms,
          p95_ms: report.stability.p95_ms,
          p99_ms: report.stability.p99_ms,
          jitter_ms: report.stability.jitter_ms
        }
      : undefined
  }

  return vm
}
