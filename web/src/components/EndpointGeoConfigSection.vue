<template>
  <div class="geo-config-section">
    <el-form label-width="90px" size="small">
      <el-form-item label="模式">
        <el-radio-group v-model="form.mode">
          <el-radio-button label="off">关闭</el-radio-button>
          <el-radio-button label="observe">观察</el-radio-button>
          <el-radio-button label="enforce">拦截</el-radio-button>
        </el-radio-group>
        <div class="cfg-hint">{{ geoModeDesc(form.mode) }}</div>
      </el-form-item>
      <el-form-item v-if="form.mode !== 'off'" label="允许国家">
        <el-select
          v-model="form.countries"
          class="geo-select"
          multiple
          clearable
          filterable
          collapse-tags
          collapse-tags-tooltip
          placeholder="留空=不判定（所有国家可拉取）"
        >
          <el-option
            v-for="c in COUNTRY_OPTIONS"
            :key="c.code"
            :label="`${c.name} (${c.code})`"
            :value="c.code"
          />
        </el-select>
        <div class="cfg-hint">
          选中的国家可拉取；留空则该维度不判定。建议先在「观察」档确认 GeoIP
          对自己设备判定准确再升「拦截」。
        </div>
      </el-form-item>
      <el-form-item v-if="form.mode !== 'off'" label="省份（暂缓）">
        <el-collapse class="province-collapse">
          <el-collapse-item name="province">
            <template #title>
              <span class="province-title">
                <el-icon><WarningFilled /></el-icon>
                省份配置（当前内嵌库无省级数据，暂不生效）
              </span>
            </template>
            <el-alert type="warning" :closable="false" show-icon class="province-alert">
              当前内嵌 GeoIP 库为 Country 级，省级数据为零。省份配置今天永不命中，拦截档下会全拒。
              <strong>请勿在拦截档使用省份配置，否则会自锁。</strong>
            </el-alert>
            <el-input
              v-model="form.provinces"
              type="textarea"
              :rows="2"
              placeholder="逗号分隔省份代码或名称（如 Guangdong, Beijing）"
              class="province-input"
            />
          </el-collapse-item>
        </el-collapse>
        <div class="cfg-hint">蜂窝网出口常落网关省；省份维度受内置库限制，当前不可用。</div>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" size="small" :loading="saving" @click="handleSave">
          保存
        </el-button>
        <el-button size="small" @click="handleReset">取消</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { WarningFilled } from '@element-plus/icons-vue'
import type { Endpoint } from '@/types'
import { updateEndpointGeoConfig } from '@/api/endpoints'
import { COUNTRY_OPTIONS, geoModeDesc, joinGeoList, parseGeoList } from '@/utils/geoconfig'

const props = defineProps<{
  endpoint: Endpoint
}>()

const emit = defineEmits<{
  (e: 'saved'): void
}>()

const form = ref<{ mode: string; countries: string[]; provinces: string }>({
  mode: 'off',
  countries: [],
  provinces: ''
})

const saving = ref(false)

const resetForm = () => {
  form.value = {
    mode: props.endpoint.geo_mode || 'off',
    countries: parseGeoList(props.endpoint.geo_countries || ''),
    provinces: props.endpoint.geo_provinces || ''
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    await updateEndpointGeoConfig(
      props.endpoint.id,
      form.value.mode,
      joinGeoList(form.value.countries),
      form.value.provinces
    )
    ElMessage.success('地域白名单已更新')
    emit('saved')
  } catch (err) {
    ElMessage.error(`更新失败：${err instanceof Error ? err.message : String(err)}`)
  } finally {
    saving.value = false
  }
}

const handleReset = () => {
  resetForm()
}

// 端点切换时重置表单
watch(() => props.endpoint, resetForm, { immediate: true })
</script>

<style scoped>
.geo-config-section {
  width: 100%;
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.geo-select {
  width: 100%;
}
.province-collapse {
  width: 100%;
  border: none;
}
.province-title {
  display: flex;
  align-items: center;
  gap: var(--ph-space-1);
  color: var(--el-color-warning);
  font-size: var(--ph-text-sm);
}
.province-alert {
  margin-bottom: var(--ph-space-2);
}
.province-input {
  margin-top: var(--ph-space-2);
}
</style>
