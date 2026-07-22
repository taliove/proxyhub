<template>
  <div>
    <PageHeader>
      <template #description>
        订阅生成的 Clash 配置骨架（hosts / dns / proxy-groups / rules）。 用
        <code class="desc-code">{{ placeholder }}</code>
        占位当前聚合的所有节点，生成订阅时自动展开。
      </template>
      <el-button :disabled="saving" @click="handleReset">恢复默认</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
    </PageHeader>

    <el-card v-loading="loading">
      <el-alert
        v-if="errorMsg"
        class="editor-alert"
        :title="errorMsg"
        type="error"
        show-icon
        :closable="true"
        @close="errorMsg = ''"
      />

      <div ref="editorEl" class="editor"></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, shallowRef, watch } from 'vue'
import { storeToRefs } from 'pinia'
// 仅按需引入 Monaco 核心 API + YAML 语言高亮，避免打包全部语言导致体积暴涨。
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import { ElMessage, ElMessageBox } from 'element-plus'
import client from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import { useLayoutStore } from '@/stores/layout'

const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)
// Monaco 主题跟随全局明暗态：亮='vs'，暗='vs-dark'
const monacoTheme = () => (isDark.value ? 'vs-dark' : 'vs')

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
    const data = await client.get<unknown, { template: string }>('/settings/template')
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
  } catch (e) {
    // 后端对 YAML 格式错误返回 400 + { error }
    const detail = (e as { response?: { data?: { error?: string } } })?.response?.data?.error
    errorMsg.value = detail || '保存失败'
  } finally {
    saving.value = false
  }
}

async function handleReset() {
  try {
    await ElMessageBox.confirm('确定恢复为默认模板吗？当前的自定义内容将被覆盖。', '恢复默认模板', {
      type: 'warning',
      confirmButtonText: '恢复默认',
      cancelButtonText: '取消'
    })
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
      theme: monacoTheme(),
      automaticLayout: true,
      minimap: { enabled: true },
      scrollBeyondLastLine: false,
      fontSize: 13,
      tabSize: 2
    })
  }
  await loadTemplate()
})

// 全局明暗切换时同步 Monaco 主题（编辑器实例是全局单例主题，setTheme 即时生效）
watch(isDark, () => {
  monaco.editor.setTheme(monacoTheme())
})

onBeforeUnmount(() => {
  editor.value?.dispose()
})
</script>

<style scoped>
.desc-code {
  background: var(--ph-bg-hover);
  padding: 1px var(--ph-space-1);
  border-radius: var(--ph-radius-sm);
}
.editor-alert {
  margin-bottom: var(--ph-space-3);
}
.editor {
  height: calc(100vh - 260px);
  min-height: 400px;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
}
</style>
