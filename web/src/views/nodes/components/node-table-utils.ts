import type { ScoreLevel } from '@/components/exam/stability'
import { canGenerateShareLink } from '@/composables/useNodeShare'
import type { UnifiedNode } from '../selfmerge'

// TestCommand 行内测试下拉的指令集;speedtest = 本机实测(跳转独立页,非服务端检测)。
export type TestCommand = 'quick' | 'real' | 'bandwidth' | 'exam' | 'speedtest'

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
