import axios from 'axios'
import type { Node } from '@/types'

// 与后端 subscription.SourceSelfHosted 保持一致
export const SELF_HOSTED = 'self-hosted'

export const NODE_TYPES = ['vmess', 'vless', 'trojan', 'ss', 'hysteria2', 'anytls']

export const isSelfHosted = (row: Node): boolean => row.source === SELF_HOSTED

// 解锁检测结果展示(分档/徽标/汇总)见 ./unlock。

export const formatTime = (t: string): string => (t ? new Date(t).toLocaleString('zh-CN') : '')

// API 错误文案:后端错误体是字符串,兜底用 fallback
export const apiErrorMessage = (e: unknown, fallback: string): string => {
  if (axios.isAxiosError(e)) {
    const data = e.response?.data
    if (typeof data === 'string' && data) return data
  }
  return fallback
}
