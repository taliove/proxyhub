<template>
  <el-form :model="form" label-width="180px" class="monitor-settings-form">
    <el-form-item label="订阅节点监控">
      <el-switch
        v-model="form.subscription_monitor_enabled"
        active-value="true"
        inactive-value="false"
      />
      <span class="hint">
        默认关闭。开启后按探测间隔对「各订阅地址实际下发的节点」做 TCP 探活：连续 3
        次失败判宕并飞书告警（节点仍在订阅中下发，只告警不摘除），连续 2 次成功发恢复通知。
        探测结果同时用于名称槽位的状态展示。
      </span>
    </el-form-item>
    <el-form-item v-if="form.subscription_monitor_enabled === 'true'" label="探测间隔（秒）">
      <el-input-number v-model="form.monitor_interval_sec" :min="30" />
      <span class="hint">默认 300 秒（5 分钟），最低 30 秒。改动下一轮探测即生效。</span>
    </el-form-item>
    <el-form-item>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getSettings, saveSettings } from '@/api/settings'

// 订阅节点监控(ADR 0047 / issue #99/#103)独立配置区:
// 后端 /api/settings 按键合并,本组件只读写自己的两个键(同 DirectEgressSettings 先例)。
const form = ref({
  subscription_monitor_enabled: 'false',
  monitor_interval_sec: 300
})
const saving = ref(false)

onMounted(async () => {
  try {
    const { settings } = await getSettings()
    form.value = {
      subscription_monitor_enabled: settings.subscription_monitor_enabled ?? 'false',
      monitor_interval_sec: Number(settings.monitor_interval_sec ?? 300) || 300
    }
  } catch (e) {
    ElMessage.error(`加载监控设置失败：${e instanceof Error ? e.message : String(e)}`)
  }
})

const save = async () => {
  saving.value = true
  try {
    await saveSettings({
      subscription_monitor_enabled: form.value.subscription_monitor_enabled,
      monitor_interval_sec: String(form.value.monitor_interval_sec)
    })
    ElMessage.success('保存成功')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.monitor-settings-form {
  max-width: 680px;
}
.hint {
  display: block;
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
}
</style>
