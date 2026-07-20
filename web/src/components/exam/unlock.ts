// 解锁段展示的纯计算逻辑(与渲染解耦,便于单测)。
// 三档判定 full/originals_only/blocked 对应 绿/黄/红 语义色,与批量解锁结果视觉语言一致;
// 无 level(error/inconclusive)归 unknown,用中性色。
import type { ExamUnlockResult } from '@/types'

export type UnlockLevel = 'full' | 'originals_only' | 'blocked' | 'unknown'

// unlockLevel 规范化后端 level 字段:三档已知值原样返回,其余(空/未知/error 路径)归 unknown。
export function unlockLevel(r: ExamUnlockResult): UnlockLevel {
  switch (r.level) {
    case 'full':
      return 'full'
    case 'originals_only':
      return 'originals_only'
    case 'blocked':
      return 'blocked'
    default:
      return 'unknown'
  }
}

// unlockLabel 档位中文标签;unknown 时按是否带 error 区分"不可用"/"未知"。
export function unlockLabel(r: ExamUnlockResult): string {
  switch (unlockLevel(r)) {
    case 'full':
      return '解锁'
    case 'originals_only':
      return '自制剧'
    case 'blocked':
      return '封锁'
    default:
      return r.error ? '不可用' : '未知'
  }
}

// unlockColorVar 档位对应的设计令牌变量名(随亮/暗主题变化):full 绿 / originals_only 黄 / blocked 红 / unknown 中性。
export function unlockColorVar(r: ExamUnlockResult): string {
  switch (unlockLevel(r)) {
    case 'full':
      return '--ph-success'
    case 'originals_only':
      return '--ph-warning'
    case 'blocked':
      return '--ph-danger'
    default:
      return '--ph-text-secondary'
  }
}

// regionBadge 出口国家码徽标文本(大写去空格);无区域返回空串(不渲染徽标)。
export function regionBadge(r: ExamUnlockResult): string {
  const region = r.region?.trim()
  return region ? region.toUpperCase() : ''
}

// upsertUnlockRow 以 target_name 为键插入/更新一行,返回新数组(不可变)。
// 首次到达追加;重跑体检时同名覆盖旧行,顺序保持首次出现的位置。
export function upsertUnlockRow(
  rows: ExamUnlockResult[],
  row: ExamUnlockResult
): ExamUnlockResult[] {
  const idx = rows.findIndex((r) => r.target_name === row.target_name)
  if (idx === -1) return [...rows, row]
  return rows.map((r, i) => (i === idx ? row : r))
}
