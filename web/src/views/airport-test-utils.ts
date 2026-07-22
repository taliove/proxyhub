import { relativeTimeZh, parseTimeMs } from '@/components/exam/examhistory'

// Score color thresholds for airport test results
export type ScoreColor = 'success' | 'warning' | 'danger' | 'info'

export function scoreColor(score: number | null | undefined, status?: string | null): ScoreColor {
  if (score === null || score === undefined) {
    return status === 'failed' ? 'info' : 'info'
  }
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
}

// 最近测试状态色点(与节点页 healthTone 同一语义):绿=健康(>=80),黄=一般(>=60),
// 红=低分或测试失败,灰=未测试。tone 取值与 StatusDot 组件的 StatusDotTone 对齐。
export type ScoreTone = 'success' | 'warning' | 'danger' | 'muted'

export function scoreTone(score: number | null | undefined, status?: string | null): ScoreTone {
  if (score === null || score === undefined) {
    return status === 'failed' ? 'danger' : 'muted'
  }
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
}

// 色点文案:同时作 tooltip 与 aria-label。
export function scoreToneLabel(score: number | null | undefined, status?: string | null): string {
  if (score === null || score === undefined) {
    return status === 'failed' ? '测试失败' : '未测试'
  }
  if (score >= 80) return '健康'
  if (score >= 60) return '一般'
  return '异常'
}

// Format airport test time as relative time (reusing exam history logic)
export function testTimeRelative(isoTime: string | null | undefined): string {
  if (!isoTime) return '-'
  return relativeTimeZh(parseTimeMs(isoTime))
}

// Format score display with precision
export function scoreDisplay(score: number | null | undefined): string {
  if (score === null || score === undefined) return '-'
  return score.toFixed(1)
}
