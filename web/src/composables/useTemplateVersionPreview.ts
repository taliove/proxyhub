import type { Ref } from 'vue'
import type { Template } from '@/api/templates'
import type YamlEditor from '@/components/YamlEditor.vue'
import type { useTemplateVersions } from '@/composables/useTemplateVersions'
import type { useTemplateAutosave } from '@/composables/useTemplateAutosave'

interface VersionPreviewDeps {
  versionHistory: ReturnType<typeof useTemplateVersions>
  autosave: ReturnType<typeof useTemplateAutosave>
  selectedTemplate: Ref<Template | null>
  originalContent: Ref<string>
  editorContent: Ref<string>
  previewContent: Ref<string>
  editorRef: Ref<InstanceType<typeof YamlEditor> | null>
  validationError: Ref<string>
}

// Version preview orchestration for the template editor: enter/exit preview,
// restore (persist the previewed historical version as a new save, not the
// current editor pane), and lazy version list loading. See CONTEXT.md
// "Rollback" and "Template Version".
export function useTemplateVersionPreview(deps: VersionPreviewDeps) {
  const {
    versionHistory,
    autosave,
    selectedTemplate,
    originalContent,
    editorContent,
    previewContent,
    editorRef,
    validationError
  } = deps
  const { previewingVersion } = versionHistory

  async function handleVersionCommand(command: { action: string; version: number }) {
    if (command.action === 'preview' && selectedTemplate.value) {
      await versionHistory.previewVersion(
        selectedTemplate.value.name,
        command.version,
        (content) => {
          previewContent.value = content
          validationError.value = ''
        }
      )
    }
  }

  function handleExitPreview() {
    versionHistory.exitPreview(originalContent.value, (content) => {
      editorContent.value = content
      editorRef.value?.setValue(content)
      previewContent.value = ''
      validationError.value = ''
    })
  }

  async function handleRestoreVersion() {
    if (previewingVersion.value === null || !selectedTemplate.value) return
    // Restore = persist the previewed historical version as a new save,
    // not the current editor pane (see CONTEXT.md "Rollback").
    const contentToRestore = previewContent.value
    const templateName = selectedTemplate.value.name
    await versionHistory.restoreVersion(
      templateName,
      previewingVersion.value,
      contentToRestore,
      async () => {
        originalContent.value = contentToRestore
        editorContent.value = contentToRestore
        editorRef.value?.setValue(contentToRestore)
        previewContent.value = ''
        await versionHistory.loadVersions(templateName)
      },
      async (content) => {
        const result = await autosave.triggerAutosave(templateName, content)
        return result ?? false
      }
    )
  }

  function onHistoryDropdownVisibleChange(visible: boolean) {
    if (visible && selectedTemplate.value) {
      versionHistory.loadVersions(selectedTemplate.value.name)
    }
  }

  return {
    handleVersionCommand,
    handleExitPreview,
    handleRestoreVersion,
    onHistoryDropdownVisibleChange
  }
}
