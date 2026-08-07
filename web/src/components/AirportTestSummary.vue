<template>
  <div class="airport-test-summary">
    <!-- 事实汇总(ticket 0045:label 列定宽 nowrap,地区覆盖格 span=2,
         61+ 地区串只在内容格内换行,不再把 label 列压成竖排单字) -->
    <el-descriptions
      :column="2"
      border
      size="small"
      label-width="90px"
      class="summary-block facts-block num"
    >
      <el-descriptions-item label="可用节点">
        {{ result.available_nodes }} / {{ result.total_nodes }}
      </el-descriptions-item>
      <el-descriptions-item label="平均延迟">
        {{ result.mean_latency_ms.toFixed(0) }} ms(P95 {{ result.p95_latency_ms.toFixed(0) }} ms)
      </el-descriptions-item>
      <el-descriptions-item label="地区覆盖" :span="2">
        {{ result.region_count }} 个地区
        <span v-if="regionList" class="muted">({{ regionList }})</span>
      </el-descriptions-item>
      <el-descriptions-item label="拉取健康">
        <template v-if="result.url_reachable">
          HTTP {{ result.http_status }},解析成功率
          {{ (result.parse_success_rate * 100).toFixed(1) }}%
        </template>
        <span v-else class="muted">N/A(URL 不可达)</span>
      </el-descriptions-item>
    </el-descriptions>

    <!-- 维度构成拆解:打开黑盒,直接给出各维度得分与权重 -->
    <div class="summary-subtitle">评分构成</div>
    <el-descriptions :column="1" border size="small" class="summary-block num">
      <el-descriptions-item :label="`可用率（权重 ${weightLabel(weights.availability)}）`">
        {{ result.availability_score.toFixed(1) }} 分
      </el-descriptions-item>
      <el-descriptions-item :label="`延迟表现（权重 ${weightLabel(weights.latency)}）`">
        {{ result.latency_score.toFixed(1) }} 分
      </el-descriptions-item>
      <el-descriptions-item
        :label="
          weights.fetchHealth === null
            ? '拉取健康（N/A，权重已重归一）'
            : `拉取健康（权重 ${weightLabel(weights.fetchHealth)}）`
        "
      >
        <span v-if="weights.fetchHealth === null" class="muted">N/A</span>
        <span v-else>{{ result.fetch_health_score.toFixed(1) }} 分</span>
      </el-descriptions-item>
      <el-descriptions-item :label="`地区覆盖（权重 ${weightLabel(weights.region)}）`">
        {{ result.region_score.toFixed(1) }} 分
      </el-descriptions-item>
    </el-descriptions>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { dimensionWeightsOf, weightLabel, type CompletedResult } from '@/composables/useAirportTest'

// 机场测试精简报告体(事实汇总 + 评分构成):运行模式对话框完成态与详情抽屉
// 「最近测试」段共用(自 AirportTestReport 抽出,单一事实源)。纯展示,数据由父级传入。
const props = defineProps<{
  result: CompletedResult
}>()

// 权重优先读 result 自带(评分同源落库),旧 run 回退硬编码
const weights = computed(() => dimensionWeightsOf(props.result))

const regionList = computed(() => {
  const dist = props.result.region_distribution
  if (!dist) return ''
  return Object.keys(dist).sort().join('/')
})
</script>

<style scoped>
.summary-block {
  margin-bottom: var(--ph-space-3);
}
/* el-descriptions 默认 table-layout:auto,长串(61 地区 / 串、延迟串)会把表撑出容器右溢。
   锁定表宽 100% + fixed 布局，列宽由 label-width 与均分决定，内容再在格内换行。 */
.summary-block :deep(.el-descriptions__table) {
  width: 100%;
  table-layout: fixed;
}
.summary-block :deep(.el-descriptions__content) {
  overflow-wrap: anywhere;
  word-break: break-word;
}
.summary-subtitle {
  font-weight: 600;
  font-size: var(--ph-text-sm);
  margin-bottom: var(--ph-space-2);
}
/* ticket 0045:label 列定宽不折行(配合 label-width),超长地区串只在内容格内换行 */
.facts-block :deep(.el-descriptions__label) {
  white-space: nowrap;
}
.num {
  font-variant-numeric: tabular-nums;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
