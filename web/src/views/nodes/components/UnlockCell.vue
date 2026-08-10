<template>
  <!-- 解锁:通过数摘要,悬浮展开各目标三档语义色(从 NodeTable 抽取,400 行门禁) -->
  <el-popover v-if="hasUnlock" placement="left" width="300" trigger="hover">
    <template #reference>
      <el-tag size="small" type="info" class="unlock-tag">{{ unlockSummary(row) }}</el-tag>
    </template>
    <div class="unlock-detail">
      <div v-for="item in unlockDisplayRows(row)" :key="item.target" class="unlock-item">
        <div class="unlock-target">
          <strong>{{ item.target }}</strong>
          <span class="unlock-badges">
            <el-tag v-if="item.region" size="small" type="info" effect="plain" class="region-badge">
              {{ item.region }}
            </el-tag>
            <el-tag
              v-if="isGenericVariant(item.display.variant)"
              :type="item.display.tagType"
              size="small"
            >
              {{ item.result.available ? '✓' : '✗' }}
            </el-tag>
            <el-tag v-else :type="item.display.tagType" size="small">
              {{ item.display.label }}
            </el-tag>
          </span>
        </div>
        <div class="unlock-info">
          <span v-if="item.result.available" class="muted num">{{ item.result.latency }}ms</span>
          <span v-else-if="item.display.variant === 'error'" class="muted">
            {{ item.result.error || '检测失败' }}
          </span>
          <span v-else class="error-text">{{ item.result.error || '不可用' }}</span>
        </div>
      </div>
    </div>
  </el-popover>
  <span v-else class="muted">—</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { isGenericVariant, unlockDisplayRows, unlockSummary } from '../unlock'
import type { UnifiedNode } from '../selfmerge'

const props = defineProps<{ row: UnifiedNode }>()

const hasUnlock = computed(
  () => !!props.row.unlock_results && Object.keys(props.row.unlock_results).length > 0
)
</script>

<style scoped>
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
.error-text {
  color: var(--ph-danger);
}
.unlock-tag {
  cursor: pointer;
}
.unlock-detail {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-3);
}
.unlock-item {
  border-bottom: 1px solid var(--ph-border-light);
  padding-bottom: var(--ph-space-2);
}
.unlock-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
.unlock-target {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-1);
}
.unlock-info {
  font-size: var(--ph-text-xs);
}
.unlock-badges {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.region-badge {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.04em;
}
</style>
