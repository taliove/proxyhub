import type { NameSlot } from '@/api/slots'

// 槽位状态推导与分组(卡片墙/表格筛选共用,spec-slot-observability-cards)。
export type SlotStatus = 'empty' | 'gone' | 'down' | 'online'

export const statusOf = (row: NameSlot): SlotStatus => {
  if (row.empty) return 'empty'
  if (row.node?.missing || row.node?.stale) return 'gone'
  if (row.node && !row.node.available) return 'down'
  return 'online'
}

export type SlotStatusTone = 'success' | 'warning' | 'danger' | 'info'

export const SLOT_STATUS_META: Record<SlotStatus, { label: string; tone: SlotStatusTone }> = {
  online: { label: '在线', tone: 'success' },
  down: { label: '不可用', tone: 'warning' },
  gone: { label: '已消失', tone: 'danger' },
  empty: { label: '空槽', tone: 'info' }
}

// 卡片分组顺序:异常排前(可观测视角先看有问题的)
export const SLOT_GROUP_ORDER: SlotStatus[] = ['down', 'gone', 'empty', 'online']

// 24h 连通率:全通格×1 + 部分通格×0.5,除以有数据格;无数据返回 null(展示 —)
export const uptime24h = (grid?: number[]): number | null => {
  if (!grid) return null
  const cells = grid.filter((v) => v !== 0)
  if (cells.length === 0) return null
  const score = cells.reduce((acc, v) => acc + (v === 1 ? 1 : v === 2 ? 0.5 : 0), 0)
  return Math.round((score / cells.length) * 100)
}
