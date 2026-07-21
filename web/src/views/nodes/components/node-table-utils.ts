import type { ScoreLevel } from '@/components/exam/stability'
import { canGenerateShareLink } from '@/composables/useNodeShare'
import type { UnifiedNode } from '../selfmerge'

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
