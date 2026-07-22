import axios from 'axios'
import type { Node } from '@/types'

// 与后端 subscription.SourceSelfHosted 保持一致
export const SELF_HOSTED = 'self-hosted'

export const NODE_TYPES = ['vmess', 'vless', 'trojan', 'ss', 'hysteria2', 'anytls']

export const isSelfHosted = (row: Node): boolean => row.source === SELF_HOSTED

// 解锁检测结果展示(分档/徽标/汇总)见 ./unlock。

export const formatTime = (t: string): string => (t ? new Date(t).toLocaleString('zh-CN') : '')

// 可用性判定来源文案(与后端 subscription.AvailabilitySource* 对齐,见 ticket 0016)。
// 未知/缺省值一律按"从未检测"展示,与后端兜底口径一致。
export const availabilitySourceText = (n: Node): string => {
  switch (n.availability_source) {
    case 'real':
      return '真实检测(代理请求)'
    case 'health':
      return '仅健康检查(TCP 快检)'
    default:
      return '从未检测'
  }
}

// 检测失败原因分类文案(ticket 0017;与后端 detection.FailReason* 对齐)。
// 未知/缺省值一律按"其他错误"兜底,与后端"绝不产出枚举外值"的口径一致。
const FAIL_REASON_TEXTS: Record<string, string> = {
  timeout: '连接或请求超时',
  refused: '连接被拒绝',
  unreachable: '网络不可达',
  handshake: '握手失败(TLS/协议)',
  protocol: '协议或响应错误',
  other: '其他错误'
}

export const failReasonText = (reason?: string): string => {
  if (!reason) return ''
  return FAIL_REASON_TEXTS[reason] ?? FAIL_REASON_TEXTS.other
}

// 订阅口径提示:回答"这个节点为什么没进订阅"。按优先级给出最主要的一条原因;
// 可用节点也提示仍受关键词/地区过滤影响。自建节点豁免过滤,不得误导为"因不可用被剔除"。
export const subscriptionHint = (n: Node): string => {
  if (isSelfHosted(n)) {
    return '自建节点豁免订阅过滤,无论可用与否都会进入订阅。'
  }
  if (n.blocked) {
    return '该节点未进入订阅的原因:已被加入屏蔽名单,订阅生成时被剔除。'
  }
  if (n.stale) {
    return '该节点未进入订阅的原因:已从机场订阅中消失(待清理),订阅生成时被剔除。'
  }
  if (!n.available) {
    if (n.availability_source === 'never') {
      return '该节点未进入订阅的原因:尚未跑过任何检测,可用状态为默认值;可点「检测此节点」或等下轮全量刷新(含健康检查)翻牌。'
    }
    return `该节点未进入订阅的原因:当前判定不可用(判定来源:${availabilitySourceText(n)}),订阅生成时被剔除。`
  }
  return '该节点当前可用,会进入订阅(仍受关键词黑/白名单与地区白名单过滤影响)。'
}

// API 错误文案:后端错误体是字符串,兜底用 fallback
export const apiErrorMessage = (e: unknown, fallback: string): string => {
  if (axios.isAxiosError(e)) {
    const data = e.response?.data
    if (typeof data === 'string' && data) return data
  }
  return fallback
}
