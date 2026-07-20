<template>
  <el-drawer v-model="visible" title="分发节点详情" size="640px">
    <template v-if="node">
      <el-descriptions :column="1" border size="small" class="drawer-block">
        <el-descriptions-item label="名称">{{ node.name }}</el-descriptions-item>
        <el-descriptions-item label="分发路径">{{ node.path }}</el-descriptions-item>
        <el-descriptions-item label="负载策略">
          {{ lbStrategyLabel(node.lb_strategy) }}
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="node.enabled ? 'success' : 'info'" size="small">
            {{ node.enabled ? '启用' : '禁用' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="流量">
          ↓ {{ formatBytes(node.total_download) }} / ↑ {{ formatBytes(node.total_upload) }}
        </el-descriptions-item>
        <el-descriptions-item label="连接数">{{
          node.total_connections || 0
        }}</el-descriptions-item>
      </el-descriptions>

      <div class="drawer-block">
        <div class="drawer-section-title">
          上游节点 ({{ node.upstream_node_keys?.length || 0 }})
        </div>
        <el-table v-if="upstream.length > 0" :data="upstream" size="small" border>
          <el-table-column prop="name" label="节点名称" min-width="180" show-overflow-tooltip />
          <el-table-column prop="region" label="地区" width="90" />
          <el-table-column prop="type" label="类型" width="90" />
          <el-table-column prop="source" label="来源" min-width="120" show-overflow-tooltip />
        </el-table>
        <div v-else class="muted">暂无上游节点明细</div>
      </div>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { DistributionNode } from '@/api/distribution-nodes'
import { formatBytes, lbStrategyLabel } from '../distribution-node-utils'

interface UpstreamRow {
  name: string
  region: string
  type: string
  source: string
}

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  node: DistributionNode | null
  upstreamOf: (row: DistributionNode) => UpstreamRow[]
}>()

const upstream = computed(() => (props.node ? props.upstreamOf(props.node) : []))
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.drawer-section-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
