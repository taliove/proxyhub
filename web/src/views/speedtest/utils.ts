// 本机实测页的纯函数:延迟/抖动口径、速率换算、历史按节点聚合与"直连基线"差值。
// 全部为不可变纯函数,返回新对象/新数组,不改入参。
// 术语纪律:"直连基线"勿简写为"基准"——撞 CONTEXT.md 既有「基准行」(体检打 Cloudflare 的对照行)。
import type { SpeedtestResult } from '@/api/speedtest'

// DIRECT_KEY 直连/未标注桶的 key(与后端空 node_key 落 NULL 的口径一致,读出时空串)。
export const DIRECT_KEY = ''

// LatencyMetrics 空闲延迟与抖动的聚合口径:
// 空闲延迟取最小 RTT(排掉排队/调度噪声,接近真实链路延迟);
// 抖动取相邻 RTT 差值绝对值的均值(RFC 3550 式逐跳变差)。
export interface LatencyMetrics {
  idleLatencyMs: number
  jitterMs: number
}

// computeLatencyMetrics 由一组 RTT 样本算空闲延迟/抖动;空样本返回全 0。
export function computeLatencyMetrics(rtts: readonly number[]): LatencyMetrics {
  if (rtts.length === 0) return { idleLatencyMs: 0, jitterMs: 0 }
  const idleLatencyMs = Math.min(...rtts)
  if (rtts.length === 1) return { idleLatencyMs, jitterMs: 0 }
  let jitterSum = 0
  for (let i = 1; i < rtts.length; i++) {
    jitterSum += Math.abs(rtts[i] - rtts[i - 1])
  }
  return { idleLatencyMs, jitterMs: jitterSum / (rtts.length - 1) }
}

// mbpsFromBytes 字节数 + 耗时换算 Mbps(1 Mbps = 1e6 bit/s,与后端体检口径一致)。
export function mbpsFromBytes(bytes: number, elapsedMs: number): number {
  if (elapsedMs <= 0 || bytes <= 0) return 0
  return (bytes * 8) / (elapsedMs / 1000) / 1e6
}

// NodeAggregate 一个标注桶(节点 key 或直连)的聚合视图:均值为口径,附次数与最近时间。
export interface NodeAggregate {
  nodeKey: string // '' = 直连基线桶
  count: number
  latestAt: string // 桶内最新一条的 created_at(历史本身已按时间倒序)
  downMbps: number
  upMbps: number
  idleLatencyMs: number
  jitterMs: number
}

// aggregateBucket 聚合单桶记录(均值口径);空桶返回 null。
export function aggregateBucket(
  nodeKey: string,
  records: readonly SpeedtestResult[]
): NodeAggregate | null {
  if (records.length === 0) return null
  const sum = records.reduce(
    (acc, r) => ({
      down: acc.down + r.down_mbps,
      up: acc.up + r.up_mbps,
      latency: acc.latency + r.idle_latency_ms,
      jitter: acc.jitter + r.jitter_ms
    }),
    { down: 0, up: 0, latency: 0, jitter: 0 }
  )
  const n = records.length
  const latestAt = records.reduce((max, r) => (r.created_at > max ? r.created_at : max), '')
  return {
    nodeKey,
    count: n,
    latestAt,
    downMbps: sum.down / n,
    upMbps: sum.up / n,
    idleLatencyMs: sum.latency / n,
    jitterMs: sum.jitter / n
  }
}

// AggregateView 聚合行 + 与直连基线的差值("减法"模型:节点开销 = 经节点实测 - 直连实测)。
// 直连行自身或无直连基线时 delta 为 null(UI 显示占位,不编造 0 差值)。
export interface AggregateView extends NodeAggregate {
  isDirect: boolean
  deltaDownMbps: number | null
  deltaUpMbps: number | null
  deltaLatencyMs: number | null
  deltaJitterMs: number | null
}

// toAggregateViews 历史记录 → 按节点聚合行,自动附与直连基线的差值。
// 排序:直连基线行固定最前,其余按最近实测时间倒序(最近活跃的标注排前)。
export function toAggregateViews(results: readonly SpeedtestResult[]): AggregateView[] {
  const buckets = new Map<string, SpeedtestResult[]>()
  for (const r of results) {
    const bucket = buckets.get(r.node_key)
    if (bucket) bucket.push(r)
    else buckets.set(r.node_key, [r])
  }
  const direct = aggregateBucket(DIRECT_KEY, buckets.get(DIRECT_KEY) ?? [])
  const views: AggregateView[] = []
  for (const [key, records] of buckets) {
    const agg = aggregateBucket(key, records)
    if (!agg) continue
    const isDirect = key === DIRECT_KEY
    views.push({
      ...agg,
      isDirect,
      deltaDownMbps: !isDirect && direct ? agg.downMbps - direct.downMbps : null,
      deltaUpMbps: !isDirect && direct ? agg.upMbps - direct.upMbps : null,
      deltaLatencyMs: !isDirect && direct ? agg.idleLatencyMs - direct.idleLatencyMs : null,
      deltaJitterMs: !isDirect && direct ? agg.jitterMs - direct.jitterMs : null
    })
  }
  return views.sort((a, b) => {
    if (a.isDirect) return -1
    if (b.isDirect) return 1
    return b.latestAt.localeCompare(a.latestAt)
  })
}

// isOrphanKey 判断标注节点是否已不在当前节点池(被清空/刷新冲回)。
// 孤儿历史保留不级联删,UI 标"已失效";直连桶永远不是孤儿。
export function isOrphanKey(nodeKey: string, poolKeys: ReadonlySet<string>): boolean {
  return nodeKey !== DIRECT_KEY && !poolKeys.has(nodeKey)
}

// round2 落库/展示统一两位小数(后端只校验非负有限,精度口径放前端)。
export function round2(v: number): number {
  return Math.round(v * 100) / 100
}

// formatDateTime RFC3339 → 本地 "MM-DD HH:mm:ss"(历史区时间列;非法输入原样返回)。
export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
