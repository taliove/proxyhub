<template>
  <div>
    <PageHeader>
      <!-- CONTEXT.md「全量刷新」:拉取所有启用机场+健康检查;页面主行动是「添加机场」,此动作用 default -->
      <el-button :loading="refreshing" @click="refreshNodes">
        <el-icon><Refresh /></el-icon> 全量刷新
      </el-button>
      <el-button type="primary" @click="openAddDialog">添加机场</el-button>
    </PageHeader>

    <el-card>
      <el-table v-loading="loading" :data="airports">
        <el-table-column prop="name" label="名称" />
        <el-table-column label="简称" width="110">
          <template #default="{ row }">
            <span v-if="row.abbr">{{ row.abbr }}</span>
            <el-tag v-else type="info" size="small">自动</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="订阅 URL" show-overflow-tooltip>
          <template #default="{ row }">
            <el-tag v-if="row.source_type === 'manual'" type="warning" size="small">手动</el-tag>
            <span v-else>{{ row.url }}</span>
          </template>
        </el-table-column>
        <!-- 用量信息(CONTEXT.md):剩余百分比 + 进度条 + 到期日;临期/将尽标红,无数据 "-" -->
        <el-table-column label="用量 / 到期" width="180">
          <template #default="{ row }">
            <AirportUsageCell :airport="row" />
          </template>
        </el-table-column>
        <el-table-column label="最近测试" width="180">
          <template #default="{ row }">
            <span class="test-cell">
              <StatusDot :tone="testTone(row)" :label="testToneLabel(row)" />
              <template v-if="row.last_test_score !== null && row.last_test_score !== undefined">
                <!-- 点分数 = 查看报告:打开详情抽屉并定位「最近测试」段,不重跑(ticket 0037) -->
                <span class="num score-text clickable" @click="openTestReport(row)">
                  {{ formatScore(row.last_test_score) }}
                </span>
                <span class="test-time">{{ formatTestTime(row.last_test_at) }}</span>
              </template>
              <template v-else-if="row.last_test_status === 'failed'">
                <span class="failed-test">测试失败</span>
                <span class="test-time">{{ formatTestTime(row.last_test_at) }}</span>
              </template>
              <span v-else class="no-test">-</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" label="状态" width="100">
          <template #default="{ row }">
            <StatusDot
              :tone="row.enabled ? 'success' : 'muted'"
              :label="row.enabled ? '启用' : '禁用'"
              class="state-dot"
            />
            <span>{{ row.enabled ? '启用' : '禁用' }}</span>
          </template>
        </el-table-column>
        <!-- 行内:详情/刷新 + 「测试」下拉(ticket 0042:抽样/测全部,不开抽屉即可测;
             编辑/启停/删除/二维码仍收敛进抽屉概况段) -->
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <!-- 手动机场无 URL 可拉:"刷新"语义是重新粘贴导入(CONTEXT.md「手动机场」) -->
            <el-button
              v-if="row.source_type === 'manual'"
              link
              type="primary"
              @click="openImport(row)"
              >重新粘贴</el-button
            >
            <el-button
              v-else
              link
              type="primary"
              :loading="refreshingIds.includes(row.id)"
              @click="refreshAirport(row)"
              >刷新</el-button
            >
            <AirportRowTestMenu @test="(full: boolean) => testDialog?.start(row, full)" />
          </template>
        </el-table-column>
        <!-- 空态引导(critique P2):首日无机场时指出第一步,不放默认「暂无数据」 -->
        <template #empty>
          <div class="table-empty">
            <p>还没有机场。机场是节点的来源——接入上游订阅 URL，或粘贴导出内容建手动机场。</p>
            <el-button type="primary" @click="openAddDialog">添加机场</el-button>
          </div>
        </template>
      </el-table>
    </el-card>

    <!-- 添加/编辑机场对话框(含来源选择与手动机场用量字段);行内「重新粘贴」
         与创建后引导共用粘贴导入对话框 -->
    <AirportFormDialog v-model="dialogVisible" :airport="editingAirport" @saved="onFormSaved" />
    <ManualImportDialog v-model="importVisible" :airport="importAirport" @imported="onImported" />

    <QRCodeDialog
      ref="qrDialog"
      v-model="qrDialogVisible"
      title="机场订阅二维码"
      hint="扫码导入该机场原始订阅（未经过 ProxyHub 聚合）"
    />
    <AirportDetailDrawer
      ref="detailDrawer"
      v-model="detailVisible"
      :airport="detailAirport"
      :refreshing="detailAirport ? refreshingIds.includes(detailAirport.id) : false"
      @edit="openEditDialog"
      @toggle="toggleAirport"
      @delete="deleteAirport"
      @refresh="refreshAirport"
      @import="openImport"
      @test="startTestRun"
      @qrcode="showQRCode"
      @run-test="onDrawerRunTest"
    />
    <!-- 运行模式测试对话框:只在显式动作时经 start() 发起运行;完成后刷新列表与抽屉报告 -->
    <AirportTestDialog ref="testDialog" @finished="onTestFinished" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import type { Airport } from '@/types'
import client from '@/api/client'
import { getJob } from '@/api/jobs'
import PageHeader from '@/components/PageHeader.vue'
import StatusDot from '@/components/StatusDot.vue'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import AirportTestDialog from '@/components/AirportTestDialog.vue'
import AirportRowTestMenu from '@/components/AirportRowTestMenu.vue'
import AirportDetailDrawer from '@/components/AirportDetailDrawer.vue'
import AirportFormDialog from '@/components/AirportFormDialog.vue'
import ManualImportDialog from '@/components/ManualImportDialog.vue'
import AirportUsageCell from '@/components/AirportUsageCell.vue'
import { testTimeRelative, scoreDisplay, scoreTone, scoreToneLabel } from './airport-test-utils'
import { getAirportQRContent } from './airport-utils'

const airports = ref<Airport[]>([])
const loading = ref(false)
const router = useRouter()
const refreshing = ref(false)
// 添加/编辑对话框:null = 添加模式
const dialogVisible = ref(false)
const editingAirport = ref<Airport | null>(null)

// 手动机场粘贴导入对话框(创建后引导 + 行内「重新粘贴」)
const importVisible = ref(false)
const importAirport = ref<Airport | null>(null)
const openImport = (row: Airport) => {
  importAirport.value = row
  importVisible.value = true
}
const onImported = () => {
  loadAirports()
}

// QR Code dialog
const qrDialogVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog> | null>(null)

// 测试对话框(运行模式):经暴露的 start() 显式发起,不再随打开自动重跑
const testDialog = ref<InstanceType<typeof AirportTestDialog> | null>(null)

// 详情抽屉状态:行内「详情」打开;抽屉内动作复用本页既有处理函数(事件上抛)。
const detailVisible = ref(false)
const detailAirport = ref<Airport | null>(null)
const detailDrawer = ref<InstanceType<typeof AirportDetailDrawer> | null>(null)
const openDetail = (row: Airport) => {
  detailAirport.value = row
  detailVisible.value = true
}

// 列表点最近测试分数 = 查看报告:打开抽屉并定位「最近测试」段,不发起新 run
const openTestReport = (row: Airport) => {
  openDetail(row)
  nextTick(() => detailDrawer.value?.focusReport?.())
}

// 抽屉概况段「测试」按钮:抽样重跑(显式动作)
const startTestRun = (airport: Airport) => {
  testDialog.value?.start(airport, false)
}

// 报告段「重新测试」(抽样)/「测全部」(全量)
const onDrawerRunTest = ({ airport, full }: { airport: Airport; full: boolean }) => {
  testDialog.value?.start(airport, full)
}

// 测试完成:刷新列表分数与抽屉报告段
const onTestFinished = () => {
  loadAirports()
  detailDrawer.value?.reloadReport?.()
}

const loadAirports = async () => {
  loading.value = true
  airports.value = await client.get('/airports')
  loading.value = false
  // 抽屉打开期间数据变更(启停/编辑/刷新)后,同步最新机场对象进抽屉
  if (detailAirport.value) {
    const fresh = airports.value.find((a) => a.id === detailAirport.value?.id)
    if (fresh) detailAirport.value = fresh
  }
}

const openAddDialog = () => {
  editingAirport.value = null
  dialogVisible.value = true
}

const openEditDialog = (row: Airport) => {
  editingAirport.value = row
  dialogVisible.value = true
}

// 对话框保存成功:刷新列表;新建的手动机场直接引导粘贴导入完成入池
const onFormSaved = (airport: Airport, isNew: boolean) => {
  loadAirports()
  if (isNew && airport.source_type === 'manual') {
    openImport(airport)
  }
}

const refreshNodes = async () => {
  refreshing.value = true
  try {
    await client.post('/aggregator/refresh')
    ElMessage.success('刷新任务已启动，正在打开任务中心')
    router.push({ name: 'Jobs' })
  } catch (error) {
    if ((error as { response?: { status?: number } })?.response?.status === 409) {
      ElMessage.warning('与进行中的单机场刷新冲突，请稍候再试')
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
  // 手动机场无 URL 可拉:"刷新"语义是重新粘贴导入(行内按钮已直开对话框,
  // 此处兜底详情抽屉的刷新动作)
  if (row.source_type === 'manual') {
    openImport(row)
    return
  }
  try {
    const resp = await client.post<unknown, { jobId: number; started: boolean }>(
      `/airports/${row.id}/refresh`
    )
    refreshingIds.value = [...refreshingIds.value, row.id]
    pollRefreshJob(row.id, resp.jobId, 0)
  } catch (error) {
    if ((error as { response?: { status?: number } })?.response?.status === 409) {
      ElMessage.warning('全量刷新进行中，稍后再试')
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
      ElMessage.success('单机场刷新完成，节点已入池')
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
  // 不可逆:DeleteAirport 不清池,节点在下轮全量刷新后 stale 下架(stale_semantics_test 锁定此语义)。
  await ElMessageBox.confirm(
    `删除机场「${row.name}」后不可恢复：该机场不再参与聚合，其节点将在下次全量刷新后从订阅中下架。`,
    '删除机场',
    { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
  )
  await client.delete(`/airports/${row.id}`)
  ElMessage.success('已删除')
  // 删除发生在抽屉内时,关闭抽屉(对象已不存在)
  if (detailAirport.value?.id === row.id) {
    detailVisible.value = false
    detailAirport.value = null
  }
  loadAirports()
}

const showQRCode = (airport: Airport) => {
  const content = getAirportQRContent(airport)
  qrDialog.value?.show(content)
}

// 最近测试状态色点:tone 与文案由视图模型纯函数推导(与节点页 healthTone 同手法)
const testTone = (row: Airport) => {
  return scoreTone(row.last_test_score, row.last_test_status)
}

const testToneLabel = (row: Airport) => {
  return scoreToneLabel(row.last_test_score, row.last_test_status)
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
.form-hint {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.num {
  font-variant-numeric: tabular-nums;
}
.test-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.state-dot {
  margin-right: var(--ph-space-1);
}
.score-text.clickable {
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
.table-empty {
  padding: var(--ph-space-6) 0;
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.table-empty p {
  margin: 0 0 var(--ph-space-3);
}
</style>
