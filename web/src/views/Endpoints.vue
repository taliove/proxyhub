<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-header">
          <span>订阅地址管理</span>
          <el-button type="primary" @click="dialogVisible = true">新建订阅地址</el-button>
        </div>
      </template>

      <el-table v-loading="loading" :data="endpoints" row-key="id">
        <el-table-column prop="alias" label="别名" />
        <el-table-column label="订阅 URL">
          <template #default="{ row }">
            <el-input :value="getSubscriptionUrl(row)" readonly>
              <template #append>
                <el-button @click="copyUrl(row)">复制</el-button>
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

    <!-- 访问统计详情:右侧抽屉展示某订阅地址的 IP 拉取明细 -->
    <el-drawer v-model="statsVisible" :title="`访问统计 - ${statsAlias}`" size="640px">
      <IPStatsTable v-if="statsEndpointId" :endpoint-id="statsEndpointId" />
    </el-drawer>

    <!-- 创建对话框 -->
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

    <!-- 命名设置对话框:按端点覆盖节点名称标准化(见 ADR 0012) -->
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

    <!-- 节点范围对话框:配置本订阅地址的动态筛选条件(见 internal/subfilter) -->
    <EndpointConditionsDialog
      v-model="conditionsVisible"
      :endpoint="conditionsEndpoint"
      @saved="loadEndpoints"
    />

    <!-- 预览对话框:所见即所得,与真实订阅走同一条节点池→关键词过滤→生成链 -->
    <el-dialog v-model="previewVisible" title="订阅预览" width="820px">
      <div class="preview-toolbar">
        <el-radio-group v-model="previewFormat" @change="loadPreview">
          <el-radio-button label="clash">Clash</el-radio-button>
          <el-radio-button label="v2ray">V2Ray</el-radio-button>
        </el-radio-group>
        <span class="preview-hint">
          共 {{ preview.count }} 个节点(已应用关键词过滤,与终端拉取到的完全一致)
        </span>
      </div>

      <el-table v-loading="previewLoading" :data="preview.nodes" height="260" size="small">
        <el-table-column label="名称" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.display_name || row.name }}</template>
        </el-table-column>
        <el-table-column prop="region" label="地区" width="80" />
        <el-table-column prop="latency" label="延迟(ms)" width="90" />
        <el-table-column label="解锁" width="120">
          <template #default="{ row }">
            <div v-if="row.unlock_results" class="unlock-badges">
              <el-tag
                v-for="(result, target) in row.unlock_results"
                :key="target"
                :type="result.available ? 'success' : 'info'"
                size="small"
                class="unlock-badge"
              >
                {{ target }}
              </el-tag>
            </div>
            <span v-else class="no-unlock">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="source" label="来源" width="100" show-overflow-tooltip />
        <el-table-column label="可用" width="80">
          <template #default="{ row }">
            <el-tag :type="row.available ? 'success' : 'info'" size="small">
              {{ row.available ? '可用' : '不可用' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>

      <el-input
        v-model="preview.content"
        type="textarea"
        :rows="8"
        readonly
        placeholder="(无节点内容)"
        class="preview-content"
      />

      <template #footer>
        <div class="preview-footer">
          <el-button @click="copySubscriptionLink">复制订阅链接</el-button>
          <el-button :disabled="!preview.content" @click="copyPreview">复制订阅内容</el-button>
          <el-button type="primary" @click="previewVisible = false">关闭</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import IPStatsTable from '@/components/IPStatsTable.vue'
import EndpointConditionsDialog from '@/components/EndpointConditionsDialog.vue'
import { hasConditions } from '@/utils/conditions'

const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = ref({ alias: '' })

// 访问统计进右侧抽屉(详情容器唯一入口),替代原 el-table expand 展开行
const statsVisible = ref(false)
const statsEndpointId = ref<number | null>(null)
const statsAlias = ref('')

const openStats = (row: Endpoint) => {
  statsEndpointId.value = row.id
  statsAlias.value = row.alias
  statsVisible.value = true
}

// 行内「更多」下拉命令:启用/禁用、命名设置、节点范围、删除
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

// 命名设置:按端点覆盖节点名称标准化
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

// 节点范围:配置本订阅地址的动态筛选条件(见 internal/subfilter)。对话框逻辑收敛在子组件。
const conditionsVisible = ref(false)
const conditionsEndpoint = ref<Endpoint | null>(null)

const openConditions = (row: Endpoint) => {
  conditionsEndpoint.value = row
  conditionsVisible.value = true
}

// 预览:随时查看某订阅地址当前会下发的订阅内容与节点清单,不产生拉取统计
const previewVisible = ref(false)
const previewLoading = ref(false)
const previewFormat = ref<'clash' | 'v2ray'>('clash')
const previewEndpointId = ref<number | null>(null)
interface PreviewNode {
  name: string
  display_name?: string
  region?: string
  latency?: number
  source?: string
  available: boolean
  unlock_results?: Record<string, { available: boolean; level?: string }>
  tags?: string[]
}

const preview = ref<{ count: number; content: string; nodes: PreviewNode[] }>({
  count: 0,
  content: '',
  nodes: []
})

const loadPreview = async () => {
  if (previewEndpointId.value == null) return
  previewLoading.value = true
  try {
    preview.value = await client.get(
      `/endpoints/${previewEndpointId.value}/preview?format=${previewFormat.value}`
    )
  } finally {
    previewLoading.value = false
  }
}

const previewEndpoint = async (row: Endpoint) => {
  previewEndpointId.value = row.id
  previewFormat.value = 'clash'
  previewVisible.value = true
  await loadPreview()
}

const copyPreview = () => {
  navigator.clipboard.writeText(preview.value.content)
  ElMessage.success('已复制订阅内容')
}

const copySubscriptionLink = () => {
  if (previewEndpointId.value == null) return
  const endpoint = endpoints.value.find((e) => e.id === previewEndpointId.value)
  if (endpoint) {
    const url = getSubscriptionUrl(endpoint)
    navigator.clipboard.writeText(url)
    ElMessage.success('已复制订阅链接')
  }
}

onMounted(loadEndpoints)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.row-ops {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.danger-item {
  color: var(--ph-danger);
}
.preview-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: var(--ph-space-3);
}
.preview-hint {
  margin-left: var(--ph-space-3);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.preview-content {
  margin-top: var(--ph-space-3);
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.unlock-badges {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ph-space-1);
}
.unlock-badge {
  font-size: var(--ph-text-xs);
}
.no-unlock {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}
.preview-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--ph-space-2);
}
</style>
