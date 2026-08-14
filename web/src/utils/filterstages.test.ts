import { describe, expect, it } from 'vitest'
import { firstZeroingStage, formatStageChain, stageLabel } from './filterstages'

describe('filterstages', () => {
  const stages = [
    { stage: 'pool', count: 341 },
    { stage: 'picks', count: 341 },
    { stage: 'region_whitelist', count: 341 },
    { stage: 'keyword_whitelist', count: 341 },
    { stage: 'keyword_blacklist', count: 341 },
    { stage: 'node_block', count: 341 },
    { stage: 'stale', count: 341 },
    { stage: 'availability', count: 0 },
    { stage: 'latency', count: 0 },
    { stage: 'dedupe', count: 0 }
  ]

  it('labels known stages in Chinese and passes unknown through', () => {
    expect(stageLabel('pool')).toBe('节点池')
    expect(stageLabel('availability')).toBe('可用性')
    expect(stageLabel('mystery')).toBe('mystery')
  })

  it('formats the chain as label-count pairs joined by arrows', () => {
    const chain = formatStageChain(stages)
    expect(chain).toContain('节点池 341')
    expect(chain).toContain('可用性 0')
    expect(chain.split(' → ')).toHaveLength(stages.length)
  })

  it('locates the first stage that drops the pool to zero', () => {
    const zeroing = firstZeroingStage(stages)
    expect(zeroing?.stage).toBe('availability')
  })

  it('returns null when no stage zeroes the pool', () => {
    const healthy = stages.map((s) => ({ ...s, count: 5 }))
    expect(firstZeroingStage(healthy)).toBeNull()
  })

  it('returns null when the pool itself is empty (nothing was filtered out)', () => {
    const empty = stages.map((s) => ({ ...s, count: 0 }))
    expect(firstZeroingStage(empty)).toBeNull()
  })
})
