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
        <!-- 行内操作 <=3:预览 / 统计(详情抽屉);低频与删除收进「更多」下拉 -->
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <span class="row-ops">
              <el-button link type="primary" @click="previewEndpoint(row)">预览</el-button>
              <el-button link type="primary" @click="openStats(row)">统计</el-button>
              <el-dropdown trigger="click" @command="(cmd: string) => onRowCommand(cmd, row)">
                <el-button link type="primary">
                  更多<el-icon class="el-icon--right"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="toggle">
                      {{ row.enabled ? '禁用' : '启用' }}
                    </el-dropdown-item>
                    <el-dropdown-item command="name-config">命名设置</el-dropdown-item>
                    <el-dropdown-item command="conditions">节点范围</el-dropdown-item>
                    <el-dropdown-item command="delete" divided>
                      <span class="danger-item">删除</span>
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-drawer v-model="statsVisible" :title="`访问统计 - ${statsAlias}`" size="640px">
      <IPStatsTable v-if="statsEndpointId" :endpoint-id="statsEndpointId" />
    </el-drawer>

    <el-dialog v-model="dialogVisible" title="新建订阅地址" width="500px">
      <el-form :model="form">
        <el-form-item label="别名">
          <el-input v-model="form.alias" placeholder="例如：老爸的手机" />
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

    <!-- 预览对话框:所见即所得,与真实订阅走同一条节点池→条件过滤→生成链 -->
    <EndpointPreviewDialog
      v-model="previewVisible"
      :endpoint="previewEndpointRow"
      :subscription-url="previewEndpointRow ? getSubscriptionUrl(previewEndpointRow) : ''"
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
import { ArrowDown } from '@element-plus/icons-vue'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import IPStatsTable from '@/components/IPStatsTable.vue'
import PageHeader from '@/components/PageHeader.vue'
import EndpointConditionsDialog from '@/components/EndpointConditionsDialog.vue'
import EndpointPreviewDialog from '@/components/EndpointPreviewDialog.vue'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { hasConditions } from '@/utils/conditions'

const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = ref({ alias: '' })
const statsVisible = ref(false)
const statsEndpointId = ref<number | null>(null)
const statsAlias = ref('')

const openStats = (row: Endpoint) => {
  statsEndpointId.value = row.id
  statsAlias.value = row.alias
  statsVisible.value = true
}

const onRowCommand = (cmd: string, row: Endpoint) => {
  if (cmd === 'toggle') toggleEndpoint(row)
  else if (cmd === 'name-config') openNameConfig(row)
  else if (cmd === 'conditions') openConditions(row)
  else if (cmd === 'delete') deleteEndpoint(row)
}

const loadEndpoints = async () => {
  loading.value = true
  endpoints.value = await client.get('/endpoints')
  loading.value = false
}

const getSubscriptionUrl = (row: Endpoint) => {
  return `${window.location.origin}/sub/${row.path}?token=${row.token}`
}

const copyUrl = (row: Endpoint) => {
  navigator.clipboard.writeText(getSubscriptionUrl(row))
  ElMessage.success('已复制到剪贴板')
}

const createEndpoint = async () => {
  await client.post('/endpoints', form.value)
  ElMessage.success('创建成功')
  dialogVisible.value = false
  form.value.alias = ''
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
  loadEndpoints()
}

const nameConfigVisible = ref(false)
const nameConfigId = ref<number | null>(null)
const nameConfigForm = ref<{ name_mode: '' | 'on' | 'off'; name_template: string }>({
  name_mode: '',
  name_template: ''
})

const nameModeLabel = (mode: string) =>
  mode === 'on' ? '强制开' : mode === 'off' ? '强制关' : '跟随全局'
const nameModeTag = (mode: string) =>
  mode === 'on' ? 'success' : mode === 'off' ? 'info' : 'warning'

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

// 预览:随时查看某订阅地址当前会下发的订阅内容与节点清单,不产生拉取统计。
// 对话框实现收敛在 EndpointPreviewDialog,本页只持有打开状态与目标端点。
const previewVisible = ref(false)
const previewEndpointRow = ref<Endpoint | null>(null)

const previewEndpoint = (row: Endpoint) => {
  previewEndpointRow.value = row
  previewVisible.value = true
}
const qrVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog>>()

const showSubscriptionQR = async (row: Endpoint) => {
  const url = getSubscriptionUrl(row)
  qrDialog.value?.show(url)
}

onMounted(loadEndpoints)
</script>

<style scoped>
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
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
.danger-item {
  color: var(--ph-danger);
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
</style>
