// 过滤链各阶段计数(issue #35)的展示辅助:后端 preview-conditions 返回
// stages:[{stage,count}],这里负责中文化标签、链式文案与"清零环节"定位。

export interface FilterStage {
  stage: string
  count: number
}

// 阶段标识 -> 中文标签(与后端 filteredNodesChainStats 的记录顺序一致)。
const STAGE_LABELS: Record<string, string> = {
  pool: '节点池',
  picks: '精选',
  region_whitelist: '地区白名单',
  keyword_whitelist: '关键词白名单',
  keyword_blacklist: '关键词黑名单',
  node_block: '机场屏蔽',
  stale: '已消失节点',
  availability: '可用性',
  latency: '延迟阈值',
  dedupe: '去重/精选'
}

export function stageLabel(stage: string): string {
  return STAGE_LABELS[stage] ?? stage
}

// 链式文案:「节点池 341 → 可用性 0 → …」。
export function formatStageChain(stages: FilterStage[]): string {
  return stages.map((s) => `${stageLabel(s.stage)} ${s.count}`).join(' → ')
}

// 定位把池清零的环节:pool 之后第一个从非零跌到 0 的阶段;无则 null。
// 调用方据此提示「N 个节点在「可用性」环节被全部过滤」。
export function firstZeroingStage(stages: FilterStage[]): FilterStage | null {
  for (let i = 1; i < stages.length; i++) {
    if (stages[i].count === 0 && stages[i - 1].count > 0) {
      return stages[i]
    }
  }
  return null
}
