<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between">
          <span>订阅地址管理</span>
          <el-button type="primary" @click="dialogVisible = true">新建订阅地址</el-button>
        </div>
      </template>

      <el-table v-loading="loading" :data="endpoints" row-key="id" :expand-row-keys="expandedRows">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div style="padding: 12px 24px">
              <IPStatsTable :endpoint-id="row.id" />
            </div>
          </template>
        </el-table-column>
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
        <el-table-column label="操作" width="260">
          <template #default="{ row }">
            <el-button link @click="toggleEndpoint(row)">
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="primary" @click="openNameConfig(row)">命名设置</el-button>
            <el-button link type="primary" @click="previewEndpoint(row)">预览</el-button>
            <el-button link type="primary" @click="toggleStats(row)">
              {{ isExpanded(row) ? '收起统计' : '统计' }}
            </el-button>
            <el-button link type="danger" @click="deleteEndpoint(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

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
        <el-table-column label="名称" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ row.display_name || row.name }}</template>
        </el-table-column>
        <el-table-column prop="region" label="地区" width="90" />
        <el-table-column prop="latency" label="延迟(ms)" width="100" />
        <el-table-column prop="source" label="来源" width="120" />
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
        <el-button :disabled="!preview.content" @click="copyPreview">复制内容</el-button>
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import IPStatsTable from '@/components/IPStatsTable.vue'

const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const form = ref({ alias: '' })
// 展开的统计行（el-table row-key 为 id，用字符串数组控制展开）
const expandedRows = ref<string[]>([])

const isExpanded = (row: Endpoint) => expandedRows.value.includes(String(row.id))

const toggleStats = (row: Endpoint) => {
  const key = String(row.id)
  if (expandedRows.value.includes(key)) {
    expandedRows.value = expandedRows.value.filter((k) => k !== key)
  } else {
    expandedRows.value = [...expandedRows.value, key]
  }
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

// 预览:随时查看某订阅地址当前会下发的订阅内容与节点清单,不产生拉取统计
const previewVisible = ref(false)
const previewLoading = ref(false)
const previewFormat = ref<'clash' | 'v2ray'>('clash')
const previewEndpointId = ref<number | null>(null)
const preview = ref<{ count: number; content: string; nodes: any[] }>({
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

onMounted(loadEndpoints)
</script>

<style scoped>
.preview-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
}
.preview-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.preview-content {
  margin-top: 12px;
}
.cfg-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
  margin-top: 4px;
}
</style>
