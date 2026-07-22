import { onMounted, ref } from 'vue'
import client from '@/api/client'
import { listJobs, type Job } from '@/api/jobs'
import { kindLabel, scopeLabel, statusMeta } from '@/views/jobs/jobmeta'
import type { Airport } from '@/types'

// 低分机场阈值:最近测试总分低于该值视为异常(spec 已拍板;60 分本身不算异常)。
export const LOW_SCORE_THRESHOLD = 60

// 异常时间窗:24h(封禁 IP 为当前状态,不带窗)。
const WINDOW_MS = 24 * 60 * 60 * 1000

// 审计异常事件类型(24h 窗口,与 Audit 页筛选项一致)。
const AUDIT_ALERT_TYPES = ['login_failure', 'honeypot_ban', 'threshold_ban'] as const

// 审计事件拉取上限:后端默认 50 条,24h 内异常超过该值时按类型计数会失真,
// 面板要的是聚合计数而非分页列表,显式放大上限(本地单用户场景 500 足够)。
const AUDIT_EVENTS_LIMIT = 500

const AUDIT_TYPE_LABELS: Record<string, string> = {
  login_failure: '登录失败',
  honeypot_ban: '蜜罐封禁',
  threshold_ban: '阈值封禁'
}

// AlertCategory 异常类别(决定左侧标签文案;跳转目标由 route 携带)。
export type AlertCategory = 'airport' | 'job' | 'banned' | 'audit'

// AlertItem 一条可点击的异常条目。
export interface AlertItem {
  key: string
  category: AlertCategory
  severity: 'danger' | 'warning'
  text: string
  route: string // vue-router 路由名
}

// BannedIP /audit/banned 记录(含仅失败计数未封禁的,需按 banned_until 过滤)。
interface BannedIP {
  ip: string
  fail_count: number
  banned_until: string
}

// AuditEvent 审计事件(本模块只消费 event_type 做聚合计数)。
interface AuditEvent {
  event_type: string
}

// parseTs 解析后端时间串:jobs 表 "YYYY-MM-DD HH:mm:ss"(本地)与 time.Time 的 RFC3339 均可。
// 非法输入返回 NaN,调用方按不过滤/不展示处理(不误报)。
function parseTs(t: string): number {
  if (!t) return NaN
  return new Date(t.replace(' ', 'T')).getTime()
}

// withinWindow 时间串是否在最近 windowMs 内;无法解析按不在窗内处理。
function withinWindow(t: string, windowMs: number): boolean {
  const ts = parseTs(t)
  return !Number.isNaN(ts) && Date.now() - ts <= windowMs
}

// buildAirportAlerts 低分机场:last_test_status=completed 且总分 < 阈值。
// 从未测试(无 last_test_status)不算异常,返回 untested 供弱提示;
// 测试失败(failed 等中间/失败态)既不算低分也不算未测试,交给任务类异常覆盖。
function buildAirportAlerts(airports: Airport[]): { items: AlertItem[]; untested: number } {
  const items: AlertItem[] = []
  let untested = 0
  for (const a of airports) {
    if (!a.last_test_status) {
      untested += 1
      continue
    }
    if (
      a.last_test_status === 'completed' &&
      a.last_test_score != null &&
      a.last_test_score < LOW_SCORE_THRESHOLD
    ) {
      items.push({
        key: `airport-${a.id}`,
        category: 'airport',
        severity: 'warning',
        text: `机场「${a.name}」最近测试得分 ${Math.round(a.last_test_score)} 分,低于 ${LOW_SCORE_THRESHOLD} 分`,
        route: 'Airports'
      })
    }
  }
  return { items, untested }
}

// buildJobAlerts 24h 内失败/中断任务(按 updated_at 落窗,倒序展示)。
function buildJobAlerts(jobs: Job[]): AlertItem[] {
  return jobs
    .filter((j) => withinWindow(j.updated_at, WINDOW_MS))
    .slice()
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    .map((j) => ({
      key: `job-${j.id}`,
      category: 'job' as const,
      severity: j.status === 'failed' ? ('danger' as const) : ('warning' as const),
      text: `${kindLabel(j.kind)}(${scopeLabel(j)})${statusMeta(j.status).label},${j.updated_at}`,
      route: 'Jobs'
    }))
}

// buildBannedAlerts 当前封禁中的 IP(banned_until 在未来;仅失败计数未达阈值的不算)。
function buildBannedAlerts(banned: BannedIP[]): AlertItem[] {
  const now = Date.now()
  return banned
    .filter((b) => parseTs(b.banned_until) > now)
    .map((b) => ({
      key: `banned-${b.ip}`,
      category: 'banned' as const,
      severity: 'danger' as const,
      text: `IP ${b.ip} 已被封禁(累计失败 ${b.fail_count} 次)`,
      route: 'Audit'
    }))
}

// buildAuditAlerts 24h 审计异常事件按类型聚合计数(逐条展开会刷屏)。
function buildAuditAlerts(events: AuditEvent[]): AlertItem[] {
  const counts = new Map<string, number>()
  for (const e of events) counts.set(e.event_type, (counts.get(e.event_type) ?? 0) + 1)
  return AUDIT_ALERT_TYPES.filter((t) => counts.has(t)).map((t) => ({
    key: `audit-${t}`,
    category: 'audit' as const,
    severity: 'warning' as const,
    text: `24h 内${AUDIT_TYPE_LABELS[t]} ${counts.get(t)} 次`,
    route: 'Audit'
  }))
}

// useAlertPanel 聚合四路现成接口生成异常面板数据:
// /airports(低分机场 + 未测试弱提示)、/jobs?status=failed|interrupted(24h 失败/中断任务)、
// /audit/banned(当前封禁 IP)、/audit/events(24h 审计异常)。
// 四路独立降级:单路失败只缺席该路数据(全局拦截器已提示),不拖垮整板。
export function useAlertPanel() {
  const alerts = ref<AlertItem[]>([])
  const untestedCount = ref(0)
  const loading = ref(true)

  onMounted(async () => {
    // 后端 status 过滤为单值等值匹配,failed/interrupted 分两次请求再合并
    const [airportsR, failedR, interruptedR, bannedR, eventsR] = await Promise.allSettled([
      client.get<unknown, Airport[]>('/airports'),
      listJobs({ status: 'failed' }),
      listJobs({ status: 'interrupted' }),
      client.get<unknown, { banned: BannedIP[] }>('/audit/banned'),
      client.get<unknown, { events: AuditEvent[]; total: number }>(
        `/audit/events?event_type=${AUDIT_ALERT_TYPES.join(',')}&time_range=24h&limit=${AUDIT_EVENTS_LIMIT}`
      )
    ])

    const items: AlertItem[] = []

    if (airportsR.status === 'fulfilled') {
      const r = buildAirportAlerts(airportsR.value || [])
      items.push(...r.items)
      untestedCount.value = r.untested
    }

    items.push(
      ...buildJobAlerts([
        ...(failedR.status === 'fulfilled' ? failedR.value : []),
        ...(interruptedR.status === 'fulfilled' ? interruptedR.value : [])
      ])
    )

    if (bannedR.status === 'fulfilled') {
      items.push(...buildBannedAlerts(bannedR.value?.banned || []))
    }
    if (eventsR.status === 'fulfilled') {
      items.push(...buildAuditAlerts(eventsR.value?.events || []))
    }

    alerts.value = items
    loading.value = false
  })

  return { alerts, untestedCount, loading }
}
