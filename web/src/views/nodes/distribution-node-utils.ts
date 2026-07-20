// 分发节点相关的展示辅助:字节格式化、负载策略标签、自动路径生成。

export const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

export const LB_STRATEGY_LABELS: Record<string, string> = {
  random: '随机',
  round_robin: '轮询',
  least_conn: '最少连接'
}

export const lbStrategyLabel = (strategy: string): string =>
  LB_STRATEGY_LABELS[strategy] || strategy

// 由名称生成路径:小写、空格转连字符、去非法字符,加 / 前缀
export const pathFromName = (name: string): string =>
  '/' +
  name
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
