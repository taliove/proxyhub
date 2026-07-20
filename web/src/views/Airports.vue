<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between">
          <span>机场管理</span>
          <div>
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
        <el-table-column prop="enabled" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
            <el-button link @click="toggleAirport(row)">
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="danger" @click="deleteAirport(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editMode ? '编辑机场' : '添加机场'" width="600px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="例如：机场A" />
        </el-form-item>
        <el-form-item label="订阅 URL">
          <el-input v-model="form.url" placeholder="https://..." />
        </el-form-item>
        <el-form-item label="简称">
          <el-input
            v-model="form.abbr"
            placeholder="留空则自动生成(如 极速机场 → JS)"
            maxlength="16"
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import type { Airport } from '@/types'
import client from '@/api/client'

const airports = ref<Airport[]>([])
const loading = ref(false)
const refreshing = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const form = ref({ name: '', url: '', abbr: '' })

const loadAirports = async () => {
  loading.value = true
  airports.value = await client.get('/airports')
  loading.value = false
}

const openAddDialog = () => {
  editMode.value = false
  editingId.value = null
  form.value = { name: '', url: '', abbr: '' }
  dialogVisible.value = true
}

const openEditDialog = (row: Airport) => {
  editMode.value = true
  editingId.value = row.id
  form.value = { name: row.name, url: row.url, abbr: row.abbr || '' }
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
    ElMessage.success('节点刷新任务已启动，请稍后查看节点状态')
  } catch (error: any) {
    if (error?.response?.status === 409) {
      ElMessage.warning('已有刷新任务在进行中，请稍候再试')
    } else {
      ElMessage.error('刷新失败')
    }
  } finally {
    refreshing.value = false
  }
}

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

onMounted(loadAirports)
</script>

<style scoped>
.form-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
  margin-top: 4px;
}
</style>
