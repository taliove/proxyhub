<template>
  <el-descriptions :column="2" border size="small" class="airport-test-diagnostic num">
    <el-descriptions-item label="HTTP 状态">
      <el-tag
        v-if="diagnostic.http_status > 0"
        :type="diagnostic.http_status === 200 ? 'success' : 'danger'"
        size="small"
      >
        {{ diagnostic.http_status }}
      </el-tag>
      <span v-else>-</span>
    </el-descriptions-item>
    <el-descriptions-item label="耗时"> {{ diagnostic.duration_ms }} ms </el-descriptions-item>
    <el-descriptions-item label="解析成功"> {{ diagnostic.node_count }} 节点 </el-descriptions-item>
    <el-descriptions-item label="解析失败">
      <el-tag v-if="diagnostic.parse_failures > 0" type="warning" size="small">
        {{ diagnostic.parse_failures }} 行
      </el-tag>
      <span v-else>0</span>
    </el-descriptions-item>
  </el-descriptions>
</template>

<script setup lang="ts">
import type { DiagnosticResult } from '@/composables/useAirportTest'

// 机场测试诊断数据块(HTTP 状态/耗时/解析统计):机场页测试对话框与
// 任务详情机场测试报告区共用(ticket 0026 抽出,三处复用)。
defineProps<{
  diagnostic: DiagnosticResult
}>()
</script>

<style scoped>
.airport-test-diagnostic {
  margin-bottom: var(--ph-space-5);
}

.num {
  font-variant-numeric: tabular-nums;
}
</style>
