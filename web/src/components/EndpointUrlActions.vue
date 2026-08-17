<template>
  <!-- 订阅 URL 行内动作(issue #123):复制(自动分流)+ 按格式复制/一键导入下拉 + 二维码。
       从 Endpoints.vue 抽取(400 行门禁) -->
  <div class="url-actions">
    <el-button @click="copyUrl">复制</el-button>
    <el-dropdown trigger="click" @command="(cmd: string) => copyFormatted(cmd)">
      <el-button>
        格式<el-icon class="el-icon--right"><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="base64">复制通用订阅（base64）</el-dropdown-item>
          <el-dropdown-item command="clash">复制 Clash 订阅</el-dropdown-item>
          <el-dropdown-item command="import">一键导入 Clash</el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>
    <el-button @click="emit('qrcode')">二维码</el-button>
  </div>
</template>

<script setup lang="ts">
import { ArrowDown } from '@element-plus/icons-vue'
import type { Endpoint } from '@/types'
import { copySubscriptionAuto, runSubscriptionCommand } from '@/utils/subscription-url'

const props = defineProps<{ endpoint: Endpoint }>()
const emit = defineEmits<{ (e: 'qrcode'): void }>()

// 复制(自动分流)/按格式复制/一键导入:动作统一收口在 subscription-url util
const copyUrl = () => copySubscriptionAuto(props.endpoint)
const copyFormatted = (cmd: string) => runSubscriptionCommand(props.endpoint, cmd)
</script>

<style scoped>
/* append 槽内容器:EP 默认给 append 内 el-button 设 flex:1 + margin:0 -20px(单按钮填满),
   多按钮会重叠；容器改为整体撑满 append(负边距抵消内边距),按钮均分宽度并填满高度 */
.url-actions {
  display: flex;
  align-self: stretch;
  margin: 0 -20px;
}
.url-actions .el-button {
  flex: 1;
  margin: 0;
  border: 0;
  border-radius: 0;
}
.url-actions > * + * {
  border-left: 1px solid var(--ph-border);
}
/* 格式下拉与按钮同形:撑满等分、高度贴合 */
.url-actions .el-dropdown {
  flex: 1;
  display: flex;
  align-self: stretch;
}
.url-actions .el-dropdown .el-button {
  width: 100%;
}
</style>
