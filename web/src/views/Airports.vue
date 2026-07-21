<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-header">
          <span>机场管理</span>
          <div class="header-actions">
            <el-button type="success" :loading="refreshing" @click="refreshNodes">
              <el-icon><Refresh /></el-icon> 立即刷新节点
            </el-button>
            <el-button type="primary" @click="openAddDialog">添加机场</el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="airports">
        <el-table-column prop="name" label="名称" />
        <el-table-column label="简称" width="110">
          <template #default="{ row }">
            <span v-if="row.abbr">{{ row.abbr }}</span>
            <el-tag v-else type="info" size="small">自动</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="url" label="订阅 URL" show-overflow-tooltip />
        <el-table-column label="最近测试" width="180">
          <template #default="{ row }">
            <div v-if="row.last_test_score !== null && row.last_test_score !== undefined">
              <el-tag
                :type="getScoreColor(row.last_test_score, row.last_test_status)"
                size="small"
                class="score-tag clickable"
                @click="openTestDialog(row)"
              >
                {{ formatScore(row.last_test_score) }}
              </el-tag>
              <span class="test-time">{{ formatTestTime(row.last_test_at) }}</span>
            </div>
            <span v-else-if="row.last_test_status === 'failed'" class="failed-test">
              <el-tag type="info" size="small">测试失败</el-tag>
              <span class="test-time">{{ formatTestTime(row.last_test_at) }}</span>
            </span>
            <span v-else class="no-test">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="330">
          <template #default="{ row }">
            <el-button link type="primary" @click="openTestDialog(row)">测试</el-button>
            <el-button
              link
              type="primary"
              :loading="refreshingIds.includes(row.id)"
              @click="refreshAirport(row)"
              >刷新</el-button
            >
            <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
            <el-button link @click="toggleAirport(row)">
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="primary" @click="showQRCode(row)">二维码</el-button>
            <el-button link type="danger" @click="deleteAirport(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editMode ? '编辑机场' : '添加机场'" width="600px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="例如：机场A" @input="onNameInput" />
        </el-form-item>
        <el-form-item label="订阅 URL">
          <el-input v-model="form.url" placeholder="https://..." />
        </el-form-item>
        <el-form-item label="简称">
          <el-input
            v-model="form.abbr"
            placeholder="留空则自动生成(如 极速机场 → JS)"
            maxlength="16"
            @input="abbrDirty = true"
          />
          <div class="form-hint">
            用于节点名称标准化(如 🇭🇰 香港 JS-01),留空按拼音/字母首字母自动生成
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">{{ editMode ? '保存' : '添加' }}</el-button>
      </template>
    </el-dialog>

    <QRCodeDialog
      ref="qrDialog"
      v-model="qrDialogVisible"
      title="机场订阅二维码"
      hint="扫码导入该机场原始订阅(未经过 ProxyHub 聚合)"
    />
    <AirportTestDialog v-model="testDialogVisible" :airport="testingAirport" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import type { Airport } from '@/types'
import client from '@/api/client'
import { getJob } from '@/api/jobs'
import { useDebouncedSuggest } from '@/composables/useDebouncedSuggest'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { getAirportQRContent } from './airport-utils'
import AirportTestDialog from '@/components/AirportTestDialog.vue'
import { scoreColor, testTimeRelative, scoreDisplay } from './airport-test-utils'

const airports = ref<Airport[]>([])
const loading = ref(false)
const router = useRouter()
const refreshing = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const form = ref({ name: '', url: '', abbr: '' })

// QR Code dialog
const qrDialogVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog> | null>(null)

// Test dialog state
const testDialogVisible = ref(false)
const testingAirport = ref<Airport | null>(null)

// Debounced auto-suggestion: name input -> abbr suggestion
const abbrRef = computed({
  get: () => form.value.abbr,
  set: (val) => {
    form.value.abbr = val
  }
})

const {
  isDirty: abbrDirty,
  onInput: onNameInput,
  reset: resetAbbrSuggest
} = useDebouncedSuggest(abbrRef, async () => {
  const name = form.value.name.trim()
  if (!name) {
    form.value.abbr = ''
    return null
  }
  const res = await client.get<unknown, { abbr: string }>('/airports/abbr-suggest', {
    params: { name }
  })
  return res.abbr || ''
})

const loadAirports = async () => {
  loading.value = true
  airports.value = await client.get('/airports')
  loading.value = false
}

const openAddDialog = () => {
  editMode.value = false
  editingId.value = null
  form.value = { name: '', url: '', abbr: '' }
  resetAbbrSuggest()
  dialogVisible.value = true
}

const openTestDialog = (airport: Airport) => {
  testingAirport.value = airport
  testDialogVisible.value = true
}

const openEditDialog = (row: Airport) => {
  editMode.value = true
  editingId.value = row.id
  form.value = { name: row.name, url: row.url, abbr: row.abbr || '' }
  // Existing abbr value indicates user-defined, mark as dirty to prevent overwrite
  // Empty abbr (shown as "auto" in table) allows auto-fill
  abbrDirty.value = !!row.abbr
  dialogVisible.value = true
}

const submitForm = async () => {
  if (editMode.value && editingId.value) {
    await client.put(`/airports/${editingId.value}`, form.value)
    ElMessage.success('更新成功')
  } else {
    await client.post('/airports', form.value)
    ElMessage.success('添加成功')
  }
  dialogVisible.value = false
  form.value = { name: '', url: '', abbr: '' }
  loadAirports()
}

const refreshNodes = async () => {
  refreshing.value = true
  try {
    await client.post('/aggregator/refresh')
    ElMessage.success('刷新任务已启动,正在打开任务中心')
    router.push({ name: 'Jobs' })
  } catch (error) {
    if ((error as { response?: { status?: number } })?.response?.status === 409) {
      ElMessage.warning('与进行中的单机场刷新冲突,请稍候再试')
    } else {
      ElMessage.error('刷新失败')
    }
  } finally {
    refreshing.value = false
  }
}

// 单机场刷新:只拉取入池不含健康检查(秒级)。进行中按钮 loading,轮询任务到终态提示。
const refreshingIds = ref<number[]>([])
// 轮询定时器:组件卸载时清理,防止 setTimeout 链泄漏继续打接口
const refreshPollTimers = new Map<number, ReturnType<typeof setTimeout>>()
const REFRESH_POLL_MAX = 40 // 40 x 1.5s = 60s 超时兜底

const refreshAirport = async (row: Airport) => {
  try {
    const resp = await client.post<unknown, { jobId: number; started: boolean }>(
      `/airports/${row.id}/refresh`
    )
    refreshingIds.value = [...refreshingIds.value, row.id]
    pollRefreshJob(row.id, resp.jobId, 0)
  } catch (error) {
    if ((error as { response?: { status?: number } })?.response?.status === 409) {
      ElMessage.warning('全量刷新进行中,稍后再试')
    } else {
      ElMessage.error('刷新失败')
    }
  }
}

const stopRefreshPoll = (airportId: number) => {
  const timer = refreshPollTimers.get(airportId)
  if (timer) clearTimeout(timer)
  refreshPollTimers.delete(airportId)
  refreshingIds.value = refreshingIds.value.filter((id) => id !== airportId)
}

const pollRefreshJob = async (airportId: number, jobId: number, attempt: number) => {
  try {
    const job = await getJob(jobId)
    if (job.status === 'running' && attempt < REFRESH_POLL_MAX) {
      refreshPollTimers.set(
        airportId,
        setTimeout(() => pollRefreshJob(airportId, jobId, attempt + 1), 1500)
      )
      return
    }
    if (job.status === 'done') {
      ElMessage.success('单机场刷新完成,节点已入池')
    } else if (job.status === 'failed') {
      ElMessage.error('刷新失败')
    } else if (job.status === 'interrupted') {
      ElMessage.warning('刷新被重启中断')
    } else {
      ElMessage.warning('刷新已取消')
    }
  } catch {
    // 轮询失败静默结束,任务本身不受影响
  }
  stopRefreshPoll(airportId)
}

onUnmounted(() => {
  for (const id of [...refreshPollTimers.keys()]) stopRefreshPoll(id)
})

const toggleAirport = async (row: Airport) => {
  await client.post(`/airports/${row.id}/toggle`)
  ElMessage.success(row.enabled ? '已禁用' : '已启用')
  loadAirports()
}

const deleteAirport = async (row: Airport) => {
  await ElMessageBox.confirm('确定删除此机场？', '确认')
  await client.delete(`/airports/${row.id}`)
  ElMessage.success('已删除')
  loadAirports()
}

const showQRCode = (airport: Airport) => {
  const content = getAirportQRContent(airport)
  qrDialog.value?.show(content)
}

// Format test score color based on value
const getScoreColor = (score: number | null | undefined, status?: string | null) => {
  return scoreColor(score, status)
}

// Format test score for display
const formatScore = (score: number | null | undefined) => {
  return scoreDisplay(score)
}

// Format test time as relative time
const formatTestTime = (isoTime: string | null | undefined) => {
  return testTimeRelative(isoTime)
}

onMounted(loadAirports)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  gap: var(--ph-space-2);
}
.form-hint {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.score-tag {
  margin-right: var(--ph-space-1);
}
.score-tag.clickable {
  cursor: pointer;
}
.test-time {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}
.failed-test,
.no-test {
  color: var(--ph-text-secondary);
}
</style>
