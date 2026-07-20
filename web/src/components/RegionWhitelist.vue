<template>
  <div class="region-whitelist-section">
    <h3>地区白名单</h3>
    <p class="hint">
      订阅生成时只下发白名单地区的节点（空=全部下发）。不影响节点池本身，所有机场节点仍会入池。
    </p>

    <div v-if="loading" class="loading">加载中...</div>
    <div v-else>
      <div class="region-grid">
        <el-checkbox-group v-model="selectedRegions">
          <el-checkbox
            v-for="region in availableRegions"
            :key="region.Code"
            :label="region.Code"
            class="region-checkbox"
          >
            {{ region.Name }} ({{ region.Code }})
          </el-checkbox>
        </el-checkbox-group>
      </div>

      <div class="stats" v-if="nodeStats">
        <p>
          <strong>当前节点池：</strong>
          <span v-for="(count, region) in nodeStats" :key="region" class="stat-item">
            {{ getRegionName(region) }} ×{{ count }}
          </span>
        </p>
        <p v-if="selectedRegions.length > 0">
          <strong>白名单生效后将下发：</strong>{{ filteredCount }} 个节点
        </p>
      </div>

      <el-button type="primary" @click="save" :loading="saving">保存</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'

interface Region {
  Code: string
  Name: string
}

const loading = ref(true)
const saving = ref(false)
const availableRegions = ref<Region[]>([])
const selectedRegions = ref<string[]>([])
const nodeStats = ref<Record<string, number> | null>(null)

const getRegionName = (code: string) => {
  const region = availableRegions.value.find(r => r.Code === code)
  return region ? region.Name : code
}

const filteredCount = computed(() => {
  if (!nodeStats.value || selectedRegions.value.length === 0) {
    return Object.values(nodeStats.value || {}).reduce((sum, count) => sum + count, 0)
  }
  return selectedRegions.value.reduce((sum, region) => {
    return sum + (nodeStats.value![region] || 0)
  }, 0)
})

const loadData = async () => {
  loading.value = true
  try {
    // 加载可用地区列表（注意：axios 拦截器返回 response.data，所以这里直接是数据对象）
    const regionsData = await client.get('/api/settings/regions') as any
    availableRegions.value = regionsData.regions || []

    // 加载当前白名单配置
    const whitelistData = await client.get('/api/settings/region-whitelist') as any
    selectedRegions.value = whitelistData.whitelist || []

    // 加载节点池统计
    try {
      const statsData = await client.get('/api/stats/global') as any
      nodeStats.value = statsData.byRegion || {}
    } catch (e) {
      console.warn('failed to load node stats:', e)
    }
  } catch (error: any) {
    console.error('[RegionWhitelist] loadData error:', error)
    ElMessage.error('加载失败: ' + (error.response?.data || error.message || String(error)))
  } finally {
    loading.value = false
  }
}

const save = async () => {
  saving.value = true
  try {
    await client.post('/api/settings/region-whitelist', {
      whitelist: selectedRegions.value
    })
    ElMessage.success('保存成功')
  } catch (error: any) {
    ElMessage.error('保存失败: ' + (error.message || String(error)))
  } finally {
    saving.value = false
  }
}

onMounted(loadData)
</script>

<style scoped>
.region-whitelist-section {
  margin-top: 20px;
  padding: 20px;
  border: 1px solid #eee;
  border-radius: 4px;
}

.hint {
  color: #666;
  font-size: 14px;
  margin-bottom: 16px;
}

.loading {
  text-align: center;
  padding: 20px;
  color: #999;
}

.region-grid {
  margin-bottom: 20px;
}

.region-checkbox {
  display: inline-block;
  margin-right: 20px;
  margin-bottom: 10px;
}

.stats {
  background: #f9f9f9;
  padding: 12px;
  border-radius: 4px;
  margin-bottom: 16px;
  font-size: 14px;
}

.stat-item {
  display: inline-block;
  margin-right: 12px;
  color: #409eff;
}
</style>
