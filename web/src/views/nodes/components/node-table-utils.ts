import type { ScoreLevel } from '@/components/exam/stability'
import { canGenerateShareLink } from '@/composables/useNodeShare'
import type { UnifiedNode } from '../selfmerge'

// TestCommand 行内检查下拉的指令集,与批量面同名同义(见 CONTEXT「检查动作」):
//   detect    = 出网快速检测(含解锁目标,全语义任务:解锁落库 + 重算标签)
//   stability = 出网+稳定性(SSE 弹框)
//   speedtest = 快速测速(基准下行 + 上行,SSE 弹框,基准口径)
//   exam      = 深度体检(完整四段,SSE 弹框)
// client-speedtest = 本机实测(浏览器端验收测量,跳转独立页,非服务端检测,ticket 0034)。
export type TestCommand = 'detect' | 'stability' | 'speedtest' | 'exam' | 'client-speedtest'

/**
 * Map stability score level to Element Plus tag type
 */
export function badgeTagType(level: ScoreLevel): 'success' | 'warning' | 'danger' {
  if (level === 'good') return 'success'
  if (level === 'fair') return 'warning'
  return 'danger'
}

/**
 * Check if a node can share link (protocol gate)
 */
export function canShare(row: UnifiedNode): boolean {
  return canGenerateShareLink(row)
}
