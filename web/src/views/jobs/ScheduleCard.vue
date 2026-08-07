<template>
  <el-card class="schedule-card">
    <template #header>
      <div class="card-header">
        <span>晚间标签重算</span>
      </div>
    </template>
    <el-form label-width="140px" class="schedule-form">
      <el-form-item label="启用定时重算">
        <el-switch v-model="form.retag_enabled" />
        <span class="hint">开启后每天在指定时刻对全部节点重算标签（retag_all 任务）。</span>
      </el-form-item>
      <el-form-item label="重算时刻">
        <el-time-picker
          v-model="timeValue"
          format="HH:mm"
          :clearable="false"
          placeholder="选择时刻"
        />
        <span class="hint">24 小时制，精确到分钟（如 03:30）。</span>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="saving" @click="onSave">保存</el-button>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { getSchedule, saveSchedule, type ScheduleConfig } from '@/api/jobs'
import { hhmmToDate, dateToHhmm } from './scheduletime'

const form = reactive<ScheduleConfig>({ retag_time: '03:30', retag_enabled: false })
const saving = ref(false)

// el-time-picker 以 Date 为 v-model;与 "HH:MM" 字符串双向转换。
const timeValue = computed<Date>({
  get: () => hhmmToDate(form.retag_time),
  set: (d) => {
    form.retag_time = dateToHhmm(d)
  }
})

const load = async () => {
  try {
    const cfg = await getSchedule()
    form.retag_time = cfg.retag_time || '03:30'
    form.retag_enabled = cfg.retag_enabled
  } catch {
    // 全局拦截器已提示;保留默认值
  }
}

const onSave = async () => {
  saving.value = true
  try {
    await saveSchedule({ retag_time: form.retag_time, retag_enabled: form.retag_enabled })
    ElMessage.success('已保存调度设置')
  } catch {
    // 全局拦截器已提示
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.schedule-card {
  margin-bottom: var(--ph-space-5);
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.hint {
  margin-left: var(--ph-space-3);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
