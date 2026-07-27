<template>
  <div>
    <PageHeader>
      <template #description>
        订阅生成的 Clash 配置骨架（hosts / dns / proxy-groups / rules）。 用
        <code class="desc-code">{{ placeholder }}</code>
        占位当前聚合的所有节点，生成订阅时自动展开。
      </template>
      <el-button @click="dialogVisible = true">新建模板</el-button>
      <el-button :disabled="!selectedTemplate || saving" @click="handleDelete">删除</el-button>
      <el-button
        :disabled="!selectedTemplate || selectedTemplate.is_default || saving"
        @click="handleSetDefault"
      >
        设为默认
      </el-button>
      <el-button type="primary" :loading="saving" :disabled="!selectedTemplate" @click="handleSave">
        保存
      </el-button>
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

      <div class="template-layout">
        <TemplateList
          :templates="templates"
          :selected-id="selectedTemplate?.id ?? null"
          @select="selectTemplate"
        />
        <div class="editor-wrapper">
          <div v-if="!selectedTemplate" class="editor-placeholder">
            请从左侧选择一个模板进行编辑，或新建模板
          </div>
          <!-- v-show 而非 v-if:编辑器 div 必须始终挂载,onMounted 才能初始化 Monaco;
               v-if 会在首次选中后才插入 DOM,错过初始化时机(右栏空白)。 -->
          <div v-show="selectedTemplate" ref="editorEl" class="editor"></div>
        </div>
      </div>
    </el-card>

    <el-dialog v-model="dialogVisible" title="新建模板" width="400px">
      <el-form :model="createForm" label-width="80px">
        <el-form-item label="模板名称">
          <el-input
            v-model="createForm.name"
            placeholder="例如：完整分流版"
            maxlength="50"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, shallowRef, watch } from 'vue'
import { storeToRefs } from 'pinia'
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import TemplateList from '@/components/TemplateList.vue'
import { useLayoutStore } from '@/stores/layout'
import client from '@/api/client'
import {
  listTemplates,
  getTemplate,
  createTemplate,
  updateTemplate,
  deleteTemplate,
  setDefaultTemplate,
  type Template
} from '@/api/templates'
import { extractErrorDetail } from '@/utils/errors'

const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)
const monacoTheme = () => (isDark.value ? 'vs-dark' : 'vs')

const editorEl = ref<HTMLElement | null>(null)
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor | null>(null)
const loading = ref(true)
const saving = ref(false)
const creating = ref(false)
const errorMsg = ref('')
const placeholder = '{{nodes}}'

const templates = ref<Template[]>([])
const selectedTemplate = ref<Template | null>(null)
const originalContent = ref('')
const dialogVisible = ref(false)
const createForm = ref({ name: '' })

async function loadTemplates() {
  loading.value = true
  errorMsg.value = ''
  try {
    const data = await listTemplates()
    templates.value = data.templates
    if (templates.value.length > 0 && !selectedTemplate.value) {
      const defaultTmpl = templates.value.find((t) => t.is_default) || templates.value[0]
      await selectTemplate(defaultTmpl)
    }
  } catch (e) {
    const detail = extractErrorDetail(e)
    errorMsg.value = detail || '加载模板列表失败'
  } finally {
    loading.value = false
  }
}

async function selectTemplate(tmpl: Template) {
  if (selectedTemplate.value && editor.value) {
    const currentContent = editor.value.getValue()
    if (currentContent !== originalContent.value) {
      try {
        await ElMessageBox.confirm(
          '当前模板有未保存的修改，切换后将丢失。是否继续？',
          '未保存的修改',
          {
            type: 'warning',
            confirmButtonText: '放弃修改',
            cancelButtonText: '取消'
          }
        )
      } catch {
        return
      }
    }
  }

  loading.value = true
  errorMsg.value = ''
  try {
    const fullTmpl = await getTemplate(tmpl.name)
    selectedTemplate.value = fullTmpl
    originalContent.value = fullTmpl.content || ''
    editor.value?.setValue(originalContent.value)
  } catch (e) {
    const detail = extractErrorDetail(e)
    errorMsg.value = detail || '加载模板内容失败'
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  const name = createForm.value.name.trim()
  if (!name) {
    ElMessage.error('模板名称不能为空')
    return
  }

  creating.value = true
  try {
    // 新模板以当前生效模板为底稿:库默认成员 ?? 回退链生效值(全局默认 ?? 内嵌)。
    // 注意占位符必须是带引号的列表项(- '{{nodes}}'),裸 {{nodes}} 不是合法 YAML。
    let scaffold = ''
    const defaultMember = templates.value.find((t) => t.is_default) ?? templates.value[0]
    if (defaultMember) {
      scaffold = (await getTemplate(defaultMember.name)).content ?? ''
    } else {
      const resp = await client.get<unknown, { template: string }>('/settings/template', {
        skipErrorToast: true
      })
      scaffold = resp.template
    }
    await createTemplate({ name, content: scaffold })
    ElMessage.success('模板创建成功')
    dialogVisible.value = false
    createForm.value.name = ''
    await loadTemplates()
    const newTmpl = templates.value.find((t) => t.name === name)
    if (newTmpl) {
      await selectTemplate(newTmpl)
    }
  } catch (e) {
    const detail = extractErrorDetail(e)
    if (detail?.includes('quota exceeded')) {
      ElMessage.error('模板数量已达配额上限，请删除不需要的模板后重试')
    } else if (detail?.includes('already exists')) {
      ElMessage.error('模板名称已存在')
    } else {
      ElMessage.error(detail || '创建模板失败')
    }
  } finally {
    creating.value = false
  }
}

async function handleSave() {
  if (!selectedTemplate.value) return

  const content = editor.value?.getValue() ?? ''
  if (!content.trim()) {
    errorMsg.value = '模板内容不能为空'
    return
  }

  saving.value = true
  errorMsg.value = ''
  try {
    await updateTemplate(selectedTemplate.value.name, { content })
    originalContent.value = content
    ElMessage.success('模板已保存，订阅立即生效')
  } catch (e) {
    const detail = extractErrorDetail(e)
    errorMsg.value = detail || '保存失败'
  } finally {
    saving.value = false
  }
}

async function handleDelete() {
  if (!selectedTemplate.value) return

  // 列表项自带引用数,删除确认前置展示"N 个订阅地址将改用默认模板"
  const refCount =
    templates.value.find((t) => t.name === selectedTemplate.value?.name)?.ref_count ?? 0
  const refWarning = refCount > 0 ? `,${refCount} 个订阅地址将改用默认模板` : ''

  try {
    await ElMessageBox.confirm(
      `确定删除模板「${selectedTemplate.value.name}」吗${refWarning}?此操作无法撤销。`,
      '删除模板',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消'
      }
    )
  } catch {
    return
  }

  saving.value = true
  errorMsg.value = ''
  try {
    const result = await deleteTemplate(selectedTemplate.value.name)
    if (result.ref_count > 0) {
      ElMessage.success(
        `已删除模板「${selectedTemplate.value.name}」，${result.ref_count} 个订阅地址将改用默认模板`
      )
    } else {
      ElMessage.success(`已删除模板「${selectedTemplate.value.name}」`)
    }
    selectedTemplate.value = null
    originalContent.value = ''
    editor.value?.setValue('')
    await loadTemplates()
  } catch (e) {
    const detail = extractErrorDetail(e)
    errorMsg.value = detail || '删除失败'
  } finally {
    saving.value = false
  }
}

async function handleSetDefault() {
  if (!selectedTemplate.value || selectedTemplate.value.is_default) return

  saving.value = true
  errorMsg.value = ''
  try {
    await setDefaultTemplate(selectedTemplate.value.name)
    ElMessage.success(`已将「${selectedTemplate.value.name}」设为默认模板`)
    await loadTemplates()
    const updated = templates.value.find((t) => t.name === selectedTemplate.value?.name)
    if (updated) {
      selectedTemplate.value = { ...selectedTemplate.value, is_default: true }
    }
  } catch (e) {
    const detail = extractErrorDetail(e)
    errorMsg.value = detail || '设置默认失败'
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
  await loadTemplates()
})

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

.template-layout {
  display: flex;
  gap: var(--ph-space-3);
  height: calc(100vh - 260px);
  min-height: 400px;
}

.editor-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.editor-placeholder {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
  background: var(--ph-bg-hover);
}

.editor {
  flex: 1;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
}
</style>
