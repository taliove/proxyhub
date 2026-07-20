import type { Node, UnlockResult } from '@/types'

// 解锁级别:后端专用判定填充(generic / error 结果为空)。与 detection.types.go 常量对齐。
export type UnlockLevel = 'full' | 'originals_only' | 'blocked'

// 通用检测目标:非解锁语义的基础设施探测,保持既有 可用/不可用 展示(零回归)。
const GENERIC_TARGETS = new Set(['connectivity', 'bandwidth'])

export const isGenericTarget = (target: string): boolean => GENERIC_TARGETS.has(target)

// 展示分档:三级解锁 + 通用可用/不可用 + 检测错误。
export type UnlockVariant =
  'full' | 'originals_only' | 'blocked' | 'available' | 'unavailable' | 'error'

// 通用档沿用各组件既有 ✓/✗、通过/失败 展示;其余为解锁语义档。
export const isGenericVariant = (v: UnlockVariant): boolean =>
  v === 'available' || v === 'unavailable'

export interface UnlockDisplay {
  variant: UnlockVariant
  label: string
  // el-tag type -> 语义色:success 绿 / warning 黄 / danger 红 / info 灰。
  // error 用 info(灰)与 blocked(danger 红)拉开,避免误读为封锁。
  tagType: 'success' | 'warning' | 'danger' | 'info'
}

type UnlockLike = Pick<UnlockResult, 'available' | 'error' | 'level'>

// classifyUnlock 把单条结果映射到展示分档。
// target 用于识别通用探测(connectivity/bandwidth):通用探测失败保持红色 ✗ 零回归;
// 解锁目标的检测错误(无 level 但有 error)单独成灰色档,不误判为 blocked。
export const classifyUnlock = (r: UnlockLike, target?: string): UnlockDisplay => {
  switch (r.level) {
    case 'full':
      return { variant: 'full', label: '完整解锁', tagType: 'success' }
    case 'originals_only':
      return { variant: 'originals_only', label: '仅自制剧', tagType: 'warning' }
    case 'blocked':
      return { variant: 'blocked', label: '已封锁', tagType: 'danger' }
  }
  const generic = target !== undefined && isGenericTarget(target)
  if (!generic && r.error) {
    return { variant: 'error', label: '检测失败', tagType: 'info' }
  }
  if (r.available) {
    return { variant: 'available', label: '可用', tagType: 'success' }
  }
  return { variant: 'unavailable', label: '不可用', tagType: 'danger' }
}

// normalizeRegion 徽标文案:两位国家码大写;空/空白返回 ''(调用方据此不占位)。
export const normalizeRegion = (region?: string): string => (region ?? '').trim().toUpperCase()

// 解锁检测结果展示行:由 Node.unlock_results map 展开,附带分档与规范化 region。
export interface UnlockDisplayRow {
  target: string
  result: UnlockResult
  display: UnlockDisplay
  region: string
}

export const unlockDisplayRows = (node: Node): UnlockDisplayRow[] => {
  if (!node.unlock_results) return []
  return Object.entries(node.unlock_results).map(([target, result]) => ({
    target,
    result,
    display: classifyUnlock(result, target),
    region: normalizeRegion(result.region)
  }))
}

// unlockSummary 概览:通过数 / 总数(既有行为,勿回归)。
export const unlockSummary = (node: Node): string => {
  if (!node.unlock_results) return '—'
  const results = Object.values(node.unlock_results)
  const passed = results.filter((r) => r.available).length
  return `${passed}/${results.length}`
}
