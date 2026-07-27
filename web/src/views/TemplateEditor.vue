<template>
  <div>
    <PageHeader>
      <template #description>
        订阅生成的 Clash 配置骨架（hosts / dns / proxy-groups / rules）。 用
        <code class="desc-code">{{ placeholder }}</code>
        占位当前聚合的所有节点，生成订阅时自动展开。
      </template>
      <el-button @click="dialogVisible = true">新建模板</el-button>
      <el-button :disabled="!selectedTemplate || autosaving" @click="handleDelete">删除</el-button>
      <el-button
        :disabled="!selectedTemplate || selectedTemplate.is_default || autosaving"
        @click="handleSetDefault"
      >
        设为默认
      </el-button>
      <el-dropdown
        :disabled="!selectedTemplate"
        trigger="click"
        @command="handleVersionCommand"
        @visible-change="onHistoryDropdownVisibleChange"
      >
        <el-button :disabled="!selectedTemplate">
          历史
          <el-icon class="el-icon--right"><ArrowDown /></el-icon>
        </el-button>
        <template #dropdown>
          <el-dropdown-menu v-loading="versionsLoading">
            <el-dropdown-item v-if="versions.length === 0" disabled>暂无历史版本</el-dropdown-item>
            <el-dropdown-item
              v-for="ver in versions"
              :key="ver.version"
              :command="{ action: 'preview', version: ver.version }"
              :disabled="previewingVersion === ver.version"
            >
              <div class="version-item">
                <span>版本 {{ ver.version }}</span>
                <span class="version-time">{{ formatTime(ver.created_at) }}</span>
                <el-tag v-if="ver.version === currentVersion" type="success" size="small"
                  >当前</el-tag
                >
              </div>
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <div class="autosave-status">
        <span v-if="autosaving" class="status-saving">保存中…</span>
        <span v-else-if="validationError" class="status-error" :title="validationError">
          校验失败
        </span>
        <span v-else-if="lastSavedAt" class="status-saved">
          已保存 {{ formatTime(lastSavedAt) }}
        </span>
      </div>
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

      <el-alert
        v-if="previewingVersion !== null"
        class="editor-alert preview-alert"
        type="warning"
        show-icon
        :closable="false"
      >
        <template #title>
          <span>预览版本 {{ previewingVersion }},未生效</span>
          <el-button
            type="primary"
            size="small"
            :loading="restoring"
            class="restore-btn"
            @click="handleRestoreVersion"
          >
            恢复此版本
          </el-button>
          <el-button size="small" class="exit-preview-btn" @click="handleExitPreview">
            返回当前版本
          </el-button>
        </template>
      </el-alert>

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
          <!-- v-show 而非 v-if:编辑器组件必须始终挂载,才能正确初始化;
               v-if 会在首次选中后才插入 DOM,导致编辑器无法初始化。 -->
          <YamlEditor
            v-show="selectedTemplate"
            ref="editorRef"
            v-model="editorContent"
            :is-dark="isDark"
            class="editor"
          />
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
import { ref, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { ArrowDown } from '@element-plus/icons-vue'
import PageHeader from '@/components/PageHeader.vue'
import TemplateList from '@/components/TemplateList.vue'
import YamlEditor from '@/components/YamlEditor.vue'
import { useLayoutStore } from '@/stores/layout'
import { useTemplateAutosave } from '@/composables/useTemplateAutosave'
import { useTemplateVersions } from '@/composables/useTemplateVersions'
import { useTemplateOperations } from '@/composables/useTemplateOperations'
import { useTemplateSelection } from '@/composables/useTemplateSelection'
import type { Template } from '@/api/templates'

const layout = useLayoutStore()
const { isDark } = storeToRefs(layout)

const editorRef = ref<InstanceType<typeof YamlEditor> | null>(null)
const editorContent = ref('')
const placeholder = '{{nodes}}'
const dialogVisible = ref(false)
const createForm = ref({ name: '' })
const creating = ref(false)

// Composables
const templateOps = useTemplateOperations()
const { templates } = templateOps

const selection = useTemplateSelection()
const { selectedTemplate, originalContent, loading, errorMsg } = selection

const autosave = useTemplateAutosave()
const { autosaving, validationError, lastSavedAt } = autosave

const versionHistory = useTemplateVersions()
const { versions, versionsLoading, previewingVersion, currentVersion, restoring } = versionHistory

// Format time as HH:mm:ss
function formatTime(isoString: string): string {
  const date = new Date(isoString)
  return date.toLocaleTimeString('zh-CN', { hour12: false })
}

// Setup autosave watcher
autosave.setupAutosave(
  editorContent,
  () => selectedTemplate.value?.name ?? null,
  () => previewingVersion.value === null
)

// Handle history dropdown commands
async function handleVersionCommand(command: { action: string; version: number }) {
  if (command.action === 'preview' && selectedTemplate.value) {
    await versionHistory.previewVersion(selectedTemplate.value.name, command.version, (content) => {
      editorContent.value = content
      editorRef.value?.setValue(content)
      validationError.value = ''
    })
  }
}

// Exit preview mode
function handleExitPreview() {
  versionHistory.exitPreview(originalContent.value, (content) => {
    editorContent.value = content
    editorRef.value?.setValue(content)
    validationError.value = ''
  })
}

// Restore a version
async function handleRestoreVersion() {
  if (previewingVersion.value === null || !selectedTemplate.value) return
  const contentToRestore = editorRef.value?.getValue() ?? ''
  const templateName = selectedTemplate.value.name
  await versionHistory.restoreVersion(
    templateName,
    previewingVersion.value,
    contentToRestore,
    async () => {
      originalContent.value = contentToRestore
      await versionHistory.loadVersions(templateName)
    },
    async (content) => {
      const result = await autosave.triggerAutosave(templateName, content)
      return result ?? false
    }
  )
}

// Load versions when dropdown opens
function onHistoryDropdownVisibleChange(visible: boolean) {
  if (visible && selectedTemplate.value) {
    versionHistory.loadVersions(selectedTemplate.value.name)
  }
}

async function loadTemplates() {
  const tmplList = await templateOps.loadTemplates()
  if (tmplList.length > 0 && !selectedTemplate.value) {
    const defaultTmpl = tmplList.find((t) => t.is_default) || tmplList[0]
    await selectTemplate(defaultTmpl)
  }
}

async function selectTemplate(tmpl: Template) {
  await selection.selectTemplate(
    tmpl,
    () => editorRef.value?.getValue() ?? '',
    previewingVersion.value !== null,
    () => {
      autosave.reset()
      versionHistory.reset()
    },
    (content) => {
      editorContent.value = content
      editorRef.value?.setValue(content)
    },
    () => versionHistory.loadVersions(tmpl.name)
  )
}

async function handleCreate() {
  creating.value = true
  const success = await templateOps.create(createForm.value.name.trim(), templates.value, () => {
    dialogVisible.value = false
    createForm.value.name = ''
  })
  if (success) {
    await loadTemplates()
    const newTmpl = templates.value.find((t) => t.name === createForm.value.name)
    if (newTmpl) await selectTemplate(newTmpl)
  }
  creating.value = false
}

async function handleDelete() {
  if (!selectedTemplate.value) return
  const refCount =
    templates.value.find((t) => t.name === selectedTemplate.value?.name)?.ref_count ?? 0
  autosaving.value = true
  const success = await templateOps.remove(selectedTemplate.value, refCount)
  if (success) {
    editorContent.value = ''
    editorRef.value?.setValue('')
    selection.reset()
    autosave.reset()
    versionHistory.reset()
    await loadTemplates()
  }
  autosaving.value = false
}

async function handleSetDefault() {
  if (!selectedTemplate.value || selectedTemplate.value.is_default) return
  autosaving.value = true
  const success = await templateOps.setDefault(selectedTemplate.value)
  if (success) {
    await loadTemplates()
    const updated = templates.value.find((t) => t.name === selectedTemplate.value?.name)
    if (updated) selectedTemplate.value = { ...selectedTemplate.value, is_default: true }
  }
  autosaving.value = false
}

onMounted(async () => {
  await loadTemplates()
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

.preview-alert {
  display: flex;
  align-items: center;
}

.preview-alert :deep(.el-alert__title) {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}

.restore-btn {
  margin-left: var(--ph-space-2);
}

.exit-preview-btn {
  margin-left: var(--ph-space-1);
}

.version-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  min-width: 200px;
}

.version-time {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-left: auto;
}

.autosave-status {
  margin-left: auto;
  font-size: var(--ph-text-sm);
  padding: 0 var(--ph-space-2);
}

.status-saving {
  color: var(--el-color-primary);
}

.status-saved {
  color: var(--ph-text-secondary);
}

.status-error {
  color: var(--el-color-danger);
  cursor: help;
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
}
</style>
