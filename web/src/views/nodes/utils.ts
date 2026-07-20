import axios from 'axios'
import type { Node } from '@/types'

// 与后端 subscription.SourceSelfHosted 保持一致
export const SELF_HOSTED = 'self-hosted'

export const NODE_TYPES = ['vmess', 'vless', 'trojan', 'ss', 'hysteria2', 'anytls']

export const isSelfHosted = (row: Node): boolean => row.source === SELF_HOSTED

// 解锁检测结果表行:由 Node.unlock_results map 展开
export interface UnlockRow {
  target: string
  available: boolean
  latency: number
  error?: string
}

export const unlockRows = (row: Node): UnlockRow[] => {
  if (!row.unlock_results) return []
  return Object.entries(row.unlock_results).map(([target, r]) => ({
    target,
    available: r.available,
    latency: r.latency,
    error: r.error
  }))
}

export const unlockSummary = (row: Node): string => {
  if (!row.unlock_results) return '—'
  const results = Object.values(row.unlock_results)
  const passed = results.filter((r) => r.available).length
  return `${passed}/${results.length}`
}

export const formatTime = (t: string): string => (t ? new Date(t).toLocaleString('zh-CN') : '')

// API 错误文案:后端错误体是字符串,兜底用 fallback
export const apiErrorMessage = (e: unknown, fallback: string): string => {
  if (axios.isAxiosError(e)) {
    const data = e.response?.data
    if (typeof data === 'string' && data) return data
  }
  return fallback
}
