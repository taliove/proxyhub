// 体检总进度与分段计数的纯计算逻辑(与渲染解耦,便于单测)。
// 三段加权:稳定性 30 样本、多地域 8 区、解锁 6 项;每收到一项计一个进度单位。

// 三段目标数量(与后端体检结构一致)。
export const EXAM_SAMPLE_TOTAL = 30
export const EXAM_REGION_TOTAL = 8
export const EXAM_UNLOCK_TOTAL = 6
// 进度总单位 = 三段目标数之和;每项(样本/区/解锁目标)权重为 1。
export const EXAM_TOTAL_UNITS = EXAM_SAMPLE_TOTAL + EXAM_REGION_TOTAL + EXAM_UNLOCK_TOTAL

// 体检进度计数(源自组件已维护的三段数组长度 + 稳定性段是否收尾)。
export interface ExamProgressCounts {
  samples: number
  regions: number
  unlocks: number
  // stabilityDone 稳定性段 metrics 已到达(段收尾),用于在样本未满 30 时也能推进到下一段。
  stabilityDone: boolean
}

// 体检段标识(串行:稳定性 -> 多地域 -> 解锁 -> 完成)。
export type ExamSegment = 'stability' | 'region_speed' | 'unlock' | 'done'

function clampCount(n: number, total: number): number {
  if (!Number.isFinite(n) || n < 0) return 0
  return n > total ? total : n
}

// examProgressPercent 三段加权总进度(0..100 整数);每项一个单位,超量/负数做钳制。
export function examProgressPercent(c: ExamProgressCounts): number {
  const units =
    clampCount(c.samples, EXAM_SAMPLE_TOTAL) +
    clampCount(c.regions, EXAM_REGION_TOTAL) +
    clampCount(c.unlocks, EXAM_UNLOCK_TOTAL)
  return Math.round((units / EXAM_TOTAL_UNITS) * 100)
}

// examActiveSegment 当前进行中的段:稳定性未收尾在采样段;收尾后按区/解锁计数推进。
export function examActiveSegment(c: ExamProgressCounts): ExamSegment {
  if (!c.stabilityDone) return 'stability'
  if (clampCount(c.regions, EXAM_REGION_TOTAL) < EXAM_REGION_TOTAL) return 'region_speed'
  if (clampCount(c.unlocks, EXAM_UNLOCK_TOTAL) < EXAM_UNLOCK_TOTAL) return 'unlock'
  return 'done'
}

// 分段计数结果:段标识 + 已完成/总数 + 展示文案。
export interface ExamSegmentCounter {
  segment: ExamSegment
  done: number
  total: number
  text: string
}

// 当前段的默认中文标签(可被当前项名称覆盖,如具体区域名"东京")。
const SEGMENT_LABEL: Record<Exclude<ExamSegment, 'done'>, string> = {
  stability: '采样',
  region_speed: '测速',
  unlock: '解锁'
}

// examSegmentCounter 生成当前段的计数展示(如"采样 12/30"、"东京 3/8"、"解锁 4/6")。
// opts 可传当前区域/解锁目标名覆盖默认标签,让计数带上具体项名。
export function examSegmentCounter(
  c: ExamProgressCounts,
  opts: { regionName?: string; unlockName?: string } = {}
): ExamSegmentCounter {
  const segment = examActiveSegment(c)
  if (segment === 'done') {
    return { segment, done: EXAM_TOTAL_UNITS, total: EXAM_TOTAL_UNITS, text: '完成' }
  }
  if (segment === 'stability') {
    const done = clampCount(c.samples, EXAM_SAMPLE_TOTAL)
    return {
      segment,
      done,
      total: EXAM_SAMPLE_TOTAL,
      text: `${SEGMENT_LABEL.stability} ${done}/${EXAM_SAMPLE_TOTAL}`
    }
  }
  if (segment === 'region_speed') {
    const done = clampCount(c.regions, EXAM_REGION_TOTAL)
    const label = opts.regionName?.trim() || SEGMENT_LABEL.region_speed
    return {
      segment,
      done,
      total: EXAM_REGION_TOTAL,
      text: `${label} ${done}/${EXAM_REGION_TOTAL}`
    }
  }
  const done = clampCount(c.unlocks, EXAM_UNLOCK_TOTAL)
  const label = opts.unlockName?.trim() || SEGMENT_LABEL.unlock
  return { segment, done, total: EXAM_UNLOCK_TOTAL, text: `${label} ${done}/${EXAM_UNLOCK_TOTAL}` }
}
