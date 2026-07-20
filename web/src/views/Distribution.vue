<template>
  <el-card>
    <template #header>
      <div style="display: flex; justify-content: space-between; align-items: center">
        <span>流量分发</span>
        <div>
          <el-switch
            v-model="globalEnabled"
            active-text="启用"
            inactive-text="禁用"
            style="margin-right: 12px"
            @change="handleGlobalToggle"
          />
          <el-tag :type="xrayStatus.running ? 'success' : 'danger'" size="large">
            Xray {{ xrayStatus.running ? '运行中' : '未运行' }}
          </el-tag>
        </div>
      </div>
    </template>

    <el-tabs v-model="activeTab" type="border-card">
      <el-tab-pane label="全局配置" name="config">
        <GlobalConfig :config="config" @updated="loadConfig" />
      </el-tab-pane>

      <el-tab-pane label="分发路径" name="paths">
        <DistributionPaths />
      </el-tab-pane>

      <el-tab-pane label="流量统计" name="stats">
        <DistributionStats />
      </el-tab-pane>
    </el-tabs>
  </el-card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getDistributionConfig,
  updateDistributionConfig,
  getXrayStatus,
  type DistributionConfig,
  type XrayStatus
} from '@/api/distribution'
import GlobalConfig from './distribution/GlobalConfig.vue'
import DistributionPaths from './distribution/DistributionPaths.vue'
import DistributionStats from './distribution/DistributionStats.vue'

const activeTab = ref('config')
const globalEnabled = ref(false)
const config = ref<DistributionConfig>({
  enabled: false,
  listen_port: 443,
  domain: '',
  protocol: 'vless',
  network: 'grpc',
  uuid: '',
  tls: true,
  cert_path: '',
  key_path: ''
})

const xrayStatus = ref<XrayStatus>({
  running: false,
  pid: 0,
  uptime_seconds: 0,
  version: ''
})

const loadConfig = async () => {
  try {
    config.value = await getDistributionConfig()
    globalEnabled.value = config.value.enabled
  } catch (error) {
    ElMessage.error('加载配置失败')
  }
}

const loadXrayStatus = async () => {
  try {
    xrayStatus.value = await getXrayStatus()
  } catch (error) {
    // Silently fail, status will show as not running
  }
}

const handleGlobalToggle = async (value: boolean) => {
  try {
    await updateDistributionConfig({ enabled: value })
    ElMessage.success(value ? '已启用流量分发' : '已禁用流量分发')
    await loadConfig()
    await loadXrayStatus()
  } catch (error) {
    ElMessage.error('操作失败')
    globalEnabled.value = !value
  }
}

onMounted(async () => {
  await loadConfig()
  await loadXrayStatus()
  // Refresh status every 30 seconds
  setInterval(loadXrayStatus, 30000)
})
</script>

<style scoped>
:deep(.el-tabs__content) {
  padding: 20px;
}
</style>
