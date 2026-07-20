<template>
  <el-drawer v-model="visible" title="节点详情" size="640px">
    <template v-if="node">
      <el-descriptions :column="2" border size="small" class="drawer-block">
        <el-descriptions-item label="原始名称">{{ node.name }}</el-descriptions-item>
        <el-descriptions-item label="标准名称">{{ node.display_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="服务器">{{ node.server }}</el-descriptions-item>
        <el-descriptions-item label="端口">{{ node.port }}</el-descriptions-item>
        <el-descriptions-item label="协议">{{ node.type }}</el-descriptions-item>
        <el-descriptions-item label="传输">{{ node.network || 'tcp' }}</el-descriptions-item>
        <el-descriptions-item label="TLS">{{ node.tls ? '是' : '否' }}</el-descriptions-item>
        <el-descriptions-item label="SNI">{{ node.sni || '—' }}</el-descriptions-item>
        <el-descriptions-item label="地区">{{ node.region || '—' }}</el-descriptions-item>
        <el-descriptions-item label="来源">{{ node.source }}</el-descriptions-item>
        <el-descriptions-item label="延迟">{{ node.latency }}ms</el-descriptions-item>
      </el-descriptions>

      <div class="drawer-block">
        <div class="drawer-section-title">解锁检测结果</div>
        <el-table v-if="rows.length > 0" :data="rows" size="small" border>
          <el-table-column prop="target" label="检测目标" width="140" />
          <el-table-column label="状态" width="90">
            <template #default="{ row: r }">
              <el-tag :type="r.available ? 'success' : 'danger'" size="small">
                {{ r.available ? '通过' : '失败' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="延迟" width="90">
            <template #default="{ row: r }">
              <span v-if="r.available">{{ r.latency }}ms</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="失败原因" min-width="200">
            <template #default="{ row: r }">
              <span v-if="r.available" class="muted">—</span>
              <span v-else class="error-text">{{ r.error || '不可用' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="muted">该节点暂无检测记录,可点下方「检测此节点」运行检测。</div>
      </div>

      <div v-if="node.bandwidth_down_mbps || node.bandwidth_up_mbps" class="drawer-block bw-detail">
        <span class="bw-label">带宽测试:</span>
        <el-tag size="small" type="success">
          下行 {{ (node.bandwidth_down_mbps || 0).toFixed(1) }} Mbps
        </el-tag>
        <el-tag size="small" type="success">
          上行 {{ (node.bandwidth_up_mbps || 0).toFixed(1) }} Mbps
        </el-tag>
      </div>

      <!-- 抽屉内只允许轻量操作:针对当前节点跑一次解锁检测 -->
      <div class="drawer-actions">
        <el-button type="primary" size="small" :disabled="detecting" @click="emit('detect', node)">
          检测此节点
        </el-button>
      </div>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Node } from '@/types'
import { unlockRows } from '../utils'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  node: Node | null
  detecting: boolean
}>()

const emit = defineEmits<{
  (e: 'detect', node: Node): void
}>()

const rows = computed(() => (props.node ? unlockRows(props.node) : []))
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.drawer-section-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.drawer-actions {
  margin-top: var(--ph-space-4);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.error-text {
  color: var(--ph-danger);
}
.bw-detail {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.bw-label {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
</style>
