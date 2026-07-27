<template>
  <div>
    <PageHeader>
      <el-button type="primary" @click="dialogVisible = true">新建订阅地址</el-button>
    </PageHeader>

    <el-card>
      <el-table v-loading="loading" :data="endpoints" row-key="id">
        <el-table-column prop="alias" label="别名" />
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

    <el-dialog v-model="dialogVisible" title="新建订阅地址" width="500px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="别名">
          <el-input v-model="form.alias" placeholder="例如：老爸的手机" />
        </el-form-item>
        <el-form-item label="配置模板">
          <el-select
            v-model="form.template_name"
            placeholder="跟随默认模板"
            clearable
            class="full-width"
          >
            <el-option
              v-for="tpl in templates"
              :key="tpl.name"
              :label="tpl.name"
              :value="tpl.name"
            />
          </el-select>
          <div class="cfg-hint">留空则使用用户默认模板(用户级模板库四级回退链)</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="createEndpoint">创建</el-button>
      </template>
    </el-dialog>

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
            placeholder="留空则用全局模板,如 {emoji} {region} {source_abbr}-{index}"
          />
          <div class="cfg-hint">
            变量:{emoji} {region} {region_code} {source} {source_abbr} {index}
            {original_name}。仅本订阅地址生效。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nameConfigVisible = false">取消</el-button>
        <el-button type="primary" @click="saveNameConfig">保存</el-button>
      </template>
    </el-dialog>

    <EndpointConditionsDialog
      v-model="conditionsVisible"
      :endpoint="conditionsEndpoint"
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
      @conditions="openConditions"
      @delete="deleteEndpoint"
      @qrcode="showSubscriptionQR"
      @template-changed="loadEndpoints"
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
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import EndpointConditionsDialog from '@/components/EndpointConditionsDialog.vue'
import EndpointDetailDrawer from '@/components/EndpointDetailDrawer.vue'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { hasConditions } from '@/utils/conditions'
import { nameModeLabel, nameModeTag } from '@/utils/namemode'
import { useTemplateList } from '@/composables/useTemplateList'

const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = ref({ alias: '', template_name: '' })
const { templates, loadTemplates } = useTemplateList()

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

const getSubscriptionUrl = (row: Endpoint) => {
  return `${window.location.origin}/sub/${row.path}?token=${row.token}`
}

const copyUrl = (row: Endpoint) => {
  navigator.clipboard.writeText(getSubscriptionUrl(row))
  ElMessage.success('已复制到剪贴板')
}

const createEndpoint = async () => {
  const payload: { alias: string; template_name?: string } = { alias: form.value.alias }
  if (form.value.template_name) {
    payload.template_name = form.value.template_name
  }
  await client.post('/endpoints', payload)
  ElMessage.success('创建成功')
  dialogVisible.value = false
  form.value = { alias: '', template_name: '' }
  loadEndpoints()
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
const conditionsVisible = ref(false)
const conditionsEndpoint = ref<Endpoint | null>(null)

const openConditions = (row: Endpoint) => {
  conditionsEndpoint.value = row
  conditionsVisible.value = true
}

const qrVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog>>()

const showSubscriptionQR = async (row: Endpoint) => {
  const url = getSubscriptionUrl(row)
  qrDialog.value?.show(url)
}

onMounted(() => {
  loadEndpoints()
  loadTemplates()
})
</script>

<style scoped>
/* append 槽内容器:EP 默认给 append 内 el-button 设 flex:1 + margin:0 -20px(单按钮填满),
   多按钮会重叠;容器改为整体撑满 append(负边距抵消内边距),按钮均分宽度并填满高度 */
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
.full-width {
  width: 100%;
}
</style>
