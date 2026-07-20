<template>
  <div>
    <div style="margin-bottom: 16px">
      <el-button type="primary" @click="handleCreate">新建路径</el-button>
    </div>

    <div v-loading="loading">
      <el-empty v-if="paths.length === 0" description="暂无分发路径" />

      <el-row v-else :gutter="16">
        <el-col
          v-for="path in paths"
          :key="path.id"
          :xs="24"
          :sm="12"
          :lg="8"
          style="margin-bottom: 16px"
        >
          <el-card shadow="hover">
            <template #header>
              <div style="display: flex; justify-content: space-between; align-items: center">
                <span style="font-weight: 600">{{ path.name }}</span>
                <el-switch :model-value="path.enabled" size="small" @change="handleToggle(path)" />
              </div>
            </template>

            <div style="margin-bottom: 8px">
              <el-text type="info" size="small">路径:</el-text>
              <el-text style="margin-left: 8px">{{ path.path }}</el-text>
            </div>

            <div style="margin-bottom: 8px">
              <el-text type="info" size="small">上游节点:</el-text>
              <el-text style="margin-left: 8px">{{ path.upstream_node_keys.length }} 个</el-text>
            </div>

            <div style="margin-bottom: 8px">
              <el-text type="info" size="small">负载策略:</el-text>
              <el-text style="margin-left: 8px">{{ getLbStrategyLabel(path.lb_strategy) }}</el-text>
            </div>

            <div style="margin-top: 12px; display: flex; gap: 8px">
              <el-button size="small" @click="handleEdit(path)">编辑</el-button>
              <el-button size="small" type="danger" @click="handleDelete(path)">删除</el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑路径' : '新建路径'" width="600px">
      <el-form ref="formRef" :model="formData" :rules="rules" label-width="120px">
        <el-form-item label="路径名称" prop="name">
          <el-input v-model="formData.name" placeholder="例如: 香港专线" />
        </el-form-item>

        <el-form-item label="路径前缀" prop="path">
          <el-input v-model="formData.path" placeholder="/hk" />
          <span class="hint">格式: /prefix,用于匹配入站请求路径</span>
        </el-form-item>

        <el-form-item label="上游节点" prop="upstream_node_keys">
          <NodeSelector v-model="formData.upstream_node_keys" />
        </el-form-item>

        <el-form-item label="负载策略" prop="lb_strategy">
          <el-select v-model="formData.lb_strategy" style="width: 100%">
            <el-option label="随机" value="random" />
            <el-option label="轮询" value="round_robin" />
            <el-option label="最少连接" value="least_conn" />
          </el-select>
        </el-form-item>

        <el-form-item label="启用">
          <el-switch v-model="formData.enabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">
          {{ isEdit ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import {
  listDistributionPaths,
  createDistributionPath,
  updateDistributionPath,
  deleteDistributionPath,
  toggleDistributionPath,
  type DistributionPath
} from '@/api/distribution'
import NodeSelector from './NodeSelector.vue'

const paths = ref<DistributionPath[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const submitting = ref(false)
const formRef = ref<FormInstance>()

const formData = ref<Partial<DistributionPath>>({
  name: '',
  path: '',
  upstream_node_keys: [],
  lb_strategy: 'random',
  enabled: true
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入路径名称', trigger: 'blur' }],
  path: [
    { required: true, message: '请输入路径前缀', trigger: 'blur' },
    {
      pattern: /^\/[a-zA-Z0-9_-]+$/,
      message: '格式: /prefix,只能包含字母、数字、下划线和连字符',
      trigger: 'blur'
    }
  ],
  upstream_node_keys: [
    {
      type: 'array',
      required: true,
      min: 1,
      message: '请至少选择一个上游节点',
      trigger: 'change'
    }
  ],
  lb_strategy: [{ required: true, message: '请选择负载策略', trigger: 'change' }]
}

const getLbStrategyLabel = (strategy: string) => {
  const labels: Record<string, string> = {
    random: '随机',
    round_robin: '轮询',
    least_conn: '最少连接'
  }
  return labels[strategy] || strategy
}

const loadPaths = async () => {
  loading.value = true
  try {
    paths.value = await listDistributionPaths()
  } catch (error) {
    ElMessage.error('加载路径列表失败')
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  isEdit.value = false
  formData.value = {
    name: '',
    path: '',
    upstream_node_keys: [],
    lb_strategy: 'random',
    enabled: true
  }
  dialogVisible.value = true
}

const handleEdit = (path: DistributionPath) => {
  isEdit.value = true
  formData.value = { ...path }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    submitting.value = true

    if (isEdit.value && formData.value.id) {
      await updateDistributionPath(formData.value.id, formData.value)
      ElMessage.success('路径已更新')
    } else {
      await createDistributionPath(formData.value)
      ElMessage.success('路径已创建')
    }

    dialogVisible.value = false
    await loadPaths()
  } catch (error) {
    if (error instanceof Error) {
      ElMessage.error('操作失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleToggle = async (path: DistributionPath) => {
  try {
    await toggleDistributionPath(path.id)
    ElMessage.success(path.enabled ? '已禁用' : '已启用')
    await loadPaths()
  } catch (error) {
    ElMessage.error('操作失败')
  }
}

const handleDelete = async (path: DistributionPath) => {
  try {
    await ElMessageBox.confirm(`确定删除路径「${path.name}」？`, '确认删除', { type: 'warning' })

    await deleteDistributionPath(path.id)
    ElMessage.success('已删除')
    await loadPaths()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

onMounted(loadPaths)
</script>

<style scoped>
.hint {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
</style>
