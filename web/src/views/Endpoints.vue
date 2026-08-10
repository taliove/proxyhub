<template>
  <div>
    <PageHeader>
      <el-button type="primary" @click="createVisible = true">新建订阅地址</el-button>
    </PageHeader>

    <el-card>
      <!-- 精选状态筛选(issue #87):全部 / 全量 / 已精选,前端过滤不改接口 -->
      <div class="list-toolbar">
        <el-radio-group v-model="picksFilter" size="small">
          <el-radio-button label="all">全部</el-radio-button>
          <el-radio-button label="full">全量</el-radio-button>
          <el-radio-button label="picked">已精选</el-radio-button>
        </el-radio-group>
      </div>
      <el-table v-loading="loading" :data="filteredEndpoints" row-key="id">
        <el-table-column prop="alias" label="别名" />
        <el-table-column label="公开名称" width="130">
          <template #default="{ row }">
            <span v-if="row.public_name">{{ row.public_name }}</span>
            <span v-else class="muted">未设置</span>
          </template>
        </el-table-column>
        <el-table-column label="订阅 URL">
          <template #default="{ row }">
            <el-input :value="getSubscriptionUrl(row)" readonly>
              <template #append>
                <div class="url-actions">
                  <el-button @click="copyUrl(row)">复制</el-button>
                  <el-button @click="showSubscriptionQR(row)">二维码</el-button>
                </div>
              </template>
            </el-input>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="命名" width="110">
          <template #default="{ row }">
            <el-tag :type="nameModeTag(row.name_mode)" size="small">{{
              nameModeLabel(row.name_mode)
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="节点范围" width="100">
          <template #default="{ row }">
            <el-tag :type="hasConditions(row.conditions) ? 'warning' : 'info'" size="small">
              {{ hasConditions(row.conditions) ? '自定义' : '全量' }}
            </el-tag>
          </template>
        </el-table-column>
        <!-- 精选(issue #80):点击标签打开节点选择器;空精选 = 全量 -->
        <el-table-column label="精选" width="110">
          <template #default="{ row }">
            <EndpointNodePicksTag :endpoint="row" @open="openNodePicks(row)" />
          </template>
        </el-table-column>
        <!-- 可用 x/y:池状态实时算(ADR 0028 决策 2),不依赖测试记录 -->
        <el-table-column label="可用" width="80">
          <template #default="{ row }">
            <span v-if="row.availability" class="num">
              {{ row.availability.available }}/{{ row.availability.total }}
            </span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <!-- 行内极简:只留「详情」;启停/命名/范围/删除收敛进详情抽屉概况段 -->
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建订阅地址:独立组件,持有表单与新建暂存精选链路(issue #80) -->
    <EndpointCreateDialog v-model="createVisible" @created="loadEndpoints" />

    <el-dialog v-model="nameConfigVisible" title="节点命名设置" width="560px">
      <el-form label-width="110px">
        <el-form-item label="标准化">
          <el-radio-group v-model="nameConfigForm.name_mode">
            <el-radio-button label="">跟随全局</el-radio-button>
            <el-radio-button label="on">强制开启</el-radio-button>
            <el-radio-button label="off">强制关闭</el-radio-button>
          </el-radio-group>
          <div class="cfg-hint">「跟随全局」时使用「系统设置 → 订阅设置」里的开关。</div>
        </el-form-item>
        <el-form-item v-if="nameConfigForm.name_mode !== 'off'" label="名称模板">
          <el-input
            v-model="nameConfigForm.name_template"
            placeholder="留空则用全局模板，如 {emoji} {region} {source_abbr}-{index}"
          />
          <div class="cfg-hint">
            变量：{emoji} {region} {region_code} {source} {source_abbr} {index}
            {original_name}。仅本订阅地址生效。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nameConfigVisible = false">取消</el-button>
        <el-button type="primary" @click="saveNameConfig">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="publicNameVisible" title="公开名称" width="460px">
      <el-form label-width="90px">
        <el-form-item label="公开名称">
          <el-input
            v-model="publicNameForm.public_name"
            placeholder="例如：家里宽带"
            maxlength="50"
          />
          <div class="cfg-hint">
            随订阅下发，客户端配置列表显示「ProxyHub · 公开名称」；留空则显示「ProxyHub」。
            别名为私有信息，绝不下发。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="publicNameVisible = false">取消</el-button>
        <el-button type="primary" @click="savePublicName">保存</el-button>
      </template>
    </el-dialog>

    <EndpointConditionsDialog
      v-model="conditionsVisible"
      :endpoint="conditionsEndpoint"
      @saved="loadEndpoints"
    />

    <!-- 精选节点选择器(issue #80):编辑模式(已有端点)直接 PUT 落库;
         新建暂存链路在 EndpointCreateDialog 内 -->
    <EndpointNodePicksDialog
      v-model="picksVisible"
      :endpoint="picksEndpoint"
      @saved="loadEndpoints"
    />

    <!-- 详情抽屉(四段式:概况/下发节点清单/订阅测试/拉取统计);
         概况段变更动作全部复用本页既有处理函数(事件上抛) -->
    <EndpointDetailDrawer
      v-model="detailVisible"
      :endpoint="detailEndpoint"
      :subscription-url="detailEndpoint ? getSubscriptionUrl(detailEndpoint) : ''"
      @toggle="toggleEndpoint"
      @name-config="openNameConfig"
      @public-name="openPublicName"
      @conditions="openConditions"
      @picks="openNodePicks"
      @delete="deleteEndpoint"
      @qrcode="showSubscriptionQR"
      @template-changed="loadEndpoints"
      @status-node-changed="loadEndpoints"
    />

    <!-- 订阅地址二维码:扫码导入客户端 -->
    <QRCodeDialog
      ref="qrDialog"
      v-model="qrVisible"
      title="订阅地址二维码"
      hint="使用客户端扫码即可导入订阅"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import { updateEndpointPublicName } from '@/api/endpoints'
import PageHeader from '@/components/PageHeader.vue'
import EndpointConditionsDialog from '@/components/EndpointConditionsDialog.vue'
import EndpointCreateDialog from '@/components/EndpointCreateDialog.vue'
import EndpointNodePicksDialog from '@/components/EndpointNodePicksDialog.vue'
import EndpointNodePicksTag from '@/components/EndpointNodePicksTag.vue'
import EndpointDetailDrawer from '@/components/EndpointDetailDrawer.vue'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { hasConditions } from '@/utils/conditions'
import { nameModeLabel, nameModeTag } from '@/utils/namemode'
import {
  filterEndpointsByPicks,
  type PicksStatusFilter
} from '@/components/endpoint-nodepicks-utils'
import { copyText } from '@/utils/clipboard'

const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const createVisible = ref(false)

// 精选状态筛选(issue #87):默认全部;计数口径与精选列标签一致(同一纯函数)
const picksFilter = ref<PicksStatusFilter>('all')
const filteredEndpoints = computed(() => filterEndpointsByPicks(endpoints.value, picksFilter.value))

// 详情抽屉状态:行内「详情」打开;抽屉内动作复用本页既有处理函数(事件上抛)。
const detailVisible = ref(false)
const detailEndpoint = ref<Endpoint | null>(null)
const openDetail = (row: Endpoint) => {
  detailEndpoint.value = row
  detailVisible.value = true
}

const loadEndpoints = async () => {
  loading.value = true
  endpoints.value = await client.get('/endpoints')
  loading.value = false
  // 抽屉打开期间数据变更(启停/命名/范围)后,同步最新端点对象进抽屉
  if (detailEndpoint.value) {
    const fresh = endpoints.value.find((e) => e.id === detailEndpoint.value?.id)
    if (fresh) detailEndpoint.value = fresh
  }
}

// 订阅地址挂根命名空间 /sub(issue #74):不含 Site Path,链接进客户端/日志不泄露管理面路径。
const getSubscriptionUrl = (row: Endpoint) => {
  return `${window.location.origin}/sub/${row.path}?token=${row.token}`
}

const copyUrl = async (row: Endpoint) => {
  // 降级剪贴板(局域网 http 非安全上下文无 clipboard API,见 utils/clipboard)
  await copyText(getSubscriptionUrl(row))
  ElMessage.success('已复制到剪贴板')
}

const toggleEndpoint = async (row: Endpoint) => {
  await client.post(`/endpoints/${row.id}/toggle`)
  ElMessage.success(row.enabled ? '已禁用' : '已启用')
  loadEndpoints()
}

const deleteEndpoint = async (row: Endpoint) => {
  await ElMessageBox.confirm('确定删除此订阅地址？', '确认')
  await client.delete(`/endpoints/${row.id}`)
  ElMessage.success('已删除')
  // 删除发生在抽屉内时,关闭抽屉(对象已不存在)
  if (detailEndpoint.value?.id === row.id) {
    detailVisible.value = false
    detailEndpoint.value = null
  }
  loadEndpoints()
}

const nameConfigVisible = ref(false)
const nameConfigId = ref<number | null>(null)
const nameConfigForm = ref<{ name_mode: '' | 'on' | 'off'; name_template: string }>({
  name_mode: '',
  name_template: ''
})

const openNameConfig = (row: Endpoint) => {
  nameConfigId.value = row.id
  nameConfigForm.value = { name_mode: row.name_mode || '', name_template: row.name_template || '' }
  nameConfigVisible.value = true
}

const saveNameConfig = async () => {
  if (nameConfigId.value == null) return
  await client.put(`/endpoints/${nameConfigId.value}/name-config`, nameConfigForm.value)
  ElMessage.success('命名设置已保存')
  nameConfigVisible.value = false
  loadEndpoints()
}

// 公开名称编辑(issue #38):照 name-config 链路,抽屉按钮 emit → 本页对话框 →
// PUT → 刷新。空串=清除,回到裸品牌名。
const publicNameVisible = ref(false)
const publicNameId = ref<number | null>(null)
const publicNameForm = ref({ public_name: '' })

const openPublicName = (row: Endpoint) => {
  publicNameId.value = row.id
  publicNameForm.value = { public_name: row.public_name || '' }
  publicNameVisible.value = true
}

const savePublicName = async () => {
  if (publicNameId.value == null) return
  await updateEndpointPublicName(publicNameId.value, publicNameForm.value.public_name)
  ElMessage.success('公开名称已保存')
  publicNameVisible.value = false
  loadEndpoints()
}

const conditionsVisible = ref(false)
const conditionsEndpoint = ref<Endpoint | null>(null)

const openConditions = (row: Endpoint) => {
  conditionsEndpoint.value = row
  conditionsVisible.value = true
}

// 精选节点(issue #80):列表精选标签与详情抽屉入口(issue #87)共用,编辑模式直接 PUT
const picksVisible = ref(false)
const picksEndpoint = ref<Endpoint | null>(null)
const openNodePicks = (row: Endpoint | null) => {
  picksEndpoint.value = row
  picksVisible.value = true
}

const qrVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog>>()

const showSubscriptionQR = async (row: Endpoint) => {
  const url = getSubscriptionUrl(row)
  qrDialog.value?.show(url)
}

onMounted(() => {
  loadEndpoints()
})
</script>

<style scoped>
.list-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--ph-space-3);
}
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
.url-actions .el-button + .el-button {
  border-left: 1px solid var(--el-border-color);
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.num {
  font-variant-numeric: tabular-nums;
}
.muted {
  color: var(--ph-text-secondary);
}
</style>
