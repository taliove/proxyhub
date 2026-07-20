// 体检滚动日志的纯计算逻辑(与渲染解耦,便于单测)。
// 从各类 SSE 帧生成人类可读的一行文案,组件只负责把最近 N 行渲染出来。
import type { ExamEvent } from '@/types'
import { unlockLabel, regionBadge } from './unlock'

// 滚动日志保留的最大行数(展示最近 ~8 条)。
export const EXAM_LOG_MAX = 8

function roundMs(v: number | undefined): string {
  return Number.isFinite(v) ? `${Math.round(v as number)}ms` : '-'
}

function mbps(v: number | undefined): string {
  return Number.isFinite(v) ? `${(v as number).toFixed(1)} Mbps` : '-'
}

const SECTION_DONE_TEXT: Record<string, string> = {
  stability: '稳定性采样完成',
  region_speed: '多地域测速完成',
  unlock: '解锁检测完成'
}

// examLogLine 把单帧映射为一行日志文案;无可展示载荷时返回 null(调用方跳过)。
export function examLogLine(frame: ExamEvent): string | null {
  switch (frame.phase) {
    case 'sample': {
      const s = frame.sample
      if (!s) return null
      return s.ok ? `采样 #${s.seq}: ${roundMs(s.latency_ms)}` : `采样 #${s.seq}: 超时`
    }
    case 'region': {
      const r = frame.region
      if (!r) return null
      return r.error ? `${r.name}: 失败` : `${r.name}: ${roundMs(r.ttfb_ms)} · ${mbps(r.down_mbps)}`
    }
    case 'unlock': {
      const u = frame.unlock_result
      if (!u) return null
      const badge = regionBadge(u)
      return badge
        ? `${u.target_name}: ${unlockLabel(u)} ${badge}`
        : `${u.target_name}: ${unlockLabel(u)}`
    }
    case 'section_done':
      return frame.section ? (SECTION_DONE_TEXT[frame.section] ?? null) : null
    case 'done':
      return '体检完成'
    case 'cancelled':
      return '已取消'
    case 'error':
      return `体检失败: ${frame.error || '未知错误'}`
    default:
      return null
  }
}

// appendExamLog 不可变追加一行并裁剪到最近 max 行;line 为 null 时返回原数组(恒等)。
export function appendExamLog(lines: string[], line: string | null, max = EXAM_LOG_MAX): string[] {
  if (line === null) return lines
  const next = [...lines, line]
  return next.length > max ? next.slice(next.length - max) : next
}

// buildExamLog 把整段帧序列折叠为最近 max 行(便于对完整序列做单测)。
export function buildExamLog(frames: ExamEvent[], max = EXAM_LOG_MAX): string[] {
  return frames.reduce<string[]>((acc, f) => appendExamLog(acc, examLogLine(f), max), [])
}
