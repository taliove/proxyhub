<template>
  <el-card class="quick-endpoints" shadow="never">
    <template #header>
      <span class="panel-title">订阅地址</span>
    </template>

    <div v-if="loading" class="panel-empty">加载中...</div>
    <div v-else-if="error" class="panel-empty">加载失败，请稍后再试</div>
    <div v-else-if="endpoints.length === 0" class="panel-empty">
      还没有订阅地址，<router-link class="empty-link" to="/endpoints">去创建</router-link>
    </div>

    <ul v-else class="endpoint-list">
      <li v-for="ep in endpoints" :key="ep.id" class="endpoint-item">
        <div class="endpoint-info">
          <span class="endpoint-alias">{{ ep.alias }}</span>
          <span class="endpoint-url" :title="getSubscriptionUrl(ep)">
            {{ getSubscriptionUrl(ep) }}
          </span>
        </div>
        <div class="endpoint-actions">
          <el-button link type="primary" @click="copyUrl(ep)">复制</el-button>
          <el-button link type="primary" @click="showQR(ep)">二维码</el-button>
        </div>
      </li>
    </ul>

    <!-- 订阅地址二维码:扫码导入客户端 -->
    <QRCodeDialog
      ref="qrDialog"
      v-model="qrVisible"
      title="订阅地址二维码"
      hint="使用客户端扫码即可导入订阅"
    />
  </el-card>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint } from '@/types'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { useQuickEndpoints } from '../composables/useQuickEndpoints'
import { copyText } from '@/utils/clipboard'

const { endpoints, loading, error, getSubscriptionUrl } = useQuickEndpoints()

// 复制与成功反馈照搬 Endpoints.vue 现成模式;剪贴板被拒绝时显式报错,不假报成功
const copyUrl = (row: Endpoint) => {
  copyText(getSubscriptionUrl(row))
    .then(() => ElMessage.success('已复制到剪贴板'))
    .catch(() => ElMessage.error('复制失败，请检查浏览器剪贴板权限'))
}

const qrVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog>>()

const showQR = (row: Endpoint) => {
  qrDialog.value?.show(getSubscriptionUrl(row))
}
</script>

<style scoped>
.quick-endpoints {
  border-radius: var(--ph-radius-lg);
}
.panel-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.panel-empty {
  padding: var(--ph-space-6) 0;
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-placeholder);
}
.empty-link {
  color: var(--ph-color-primary);
  text-decoration: none;
}
.endpoint-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.endpoint-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-2) 0;
}
.endpoint-item + .endpoint-item {
  border-top: 1px solid var(--ph-border-light);
}
/* min-width:0 让 flex 子项可收缩,URL 单行截断 */
.endpoint-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-1);
}
.endpoint-alias {
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.endpoint-url {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.endpoint-actions {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
  flex-shrink: 0;
}
</style>
