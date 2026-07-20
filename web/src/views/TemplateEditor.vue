<template>
  <el-card v-loading="loading">
    <template #header>
      <div class="header">
        <div>
          <span class="title">配置模板</span>
          <span class="subtitle">
            订阅生成的 Clash 配置骨架（hosts / dns / proxy-groups / rules）。
            用 <code>{{ placeholder }}</code> 占位当前聚合的所有节点，生成订阅时自动展开。
          </span>
        </div>
        <div class="actions">
          <el-button @click="handleReset" :disabled="saving">恢复默认</el-button>
          <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
        </div>
      </div>
    </template>

    <el-alert
      v-if="errorMsg"
      :title="errorMsg"
      type="error"
      show-icon
      :closable="true"
      @close="errorMsg = ''"
      style="margin-bottom: 12px;"
    />

    <div ref="editorEl" class="editor"></div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, shallowRef } from 'vue'
// 仅按需引入 Monaco 核心 API + YAML 语言高亮，避免打包全部语言导致体积暴涨。
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import { ElMessage, ElMessageBox } from 'element-plus'
import client from '@/api/client'

const editorEl = ref<HTMLElement | null>(null)
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor | null>(null)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
// 说明文字里展示的占位符字面量（避免与 Vue 的 {{ }} 插值语法冲突）
const placeholder = '{{nodes}}'

// 加载当前模板并把内容灌入编辑器
async function loadTemplate() {
  loading.value = true
  try {
    const data = await client.get<any, { template: string }>('/settings/template')
    editor.value?.setValue(data.template ?? '')
  } catch {
    errorMsg.value = '加载模板失败'
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  const template = editor.value?.getValue() ?? ''
  if (!template.trim()) {
    errorMsg.value = '模板不能为空'
    return
  }
  saving.value = true
  errorMsg.value = ''
  try {
    await client.put('/settings/template', { template })
    ElMessage.success('模板已保存，订阅立即生效')
  } catch (e: any) {
    // 后端对 YAML 格式错误返回 400 + { error }
    errorMsg.value = e?.response?.data?.error || '保存失败'
  } finally {
    saving.value = false
  }
}

async function handleReset() {
  try {
    await ElMessageBox.confirm(
      '确定恢复为默认模板吗？当前的自定义内容将被覆盖。',
      '恢复默认模板',
      { type: 'warning', confirmButtonText: '恢复默认', cancelButtonText: '取消' }
    )
  } catch {
    return // 用户取消
  }
  saving.value = true
  errorMsg.value = ''
  try {
    await client.post('/settings/template/reset')
    await loadTemplate()
    ElMessage.success('已恢复默认模板')
  } catch {
    errorMsg.value = '恢复默认失败'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  if (editorEl.value) {
    editor.value = monaco.editor.create(editorEl.value, {
      value: '',
      language: 'yaml',
      theme: 'vs',
      automaticLayout: true,
      minimap: { enabled: true },
      scrollBeyondLastLine: false,
      fontSize: 13,
      tabSize: 2
    })
  }
  await loadTemplate()
})

onBeforeUnmount(() => {
  editor.value?.dispose()
})
</script>

<style scoped>
.header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}
.title {
  font-size: 16px;
  font-weight: 600;
  margin-right: 12px;
}
.subtitle {
  font-size: 12px;
  color: #909399;
}
.subtitle code {
  background: #f0f2f5;
  padding: 1px 4px;
  border-radius: 3px;
}
.actions {
  flex-shrink: 0;
}
.editor {
  height: calc(100vh - 260px);
  min-height: 400px;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
}
</style>
