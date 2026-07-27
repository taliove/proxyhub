// Template selection and switching composable
import { ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { getTemplate, type Template } from '@/api/templates'
import { extractErrorDetail } from '@/utils/errors'

export function useTemplateSelection() {
  const selectedTemplate = ref<Template | null>(null)
  const originalContent = ref('')
  const loading = ref(false)
  const errorMsg = ref('')

  // Check if there are unsaved changes
  function hasUnsavedChanges(currentContent: string, isInPreview: boolean): boolean {
    return currentContent !== originalContent.value || isInPreview
  }

  // Confirm switching template with unsaved changes
  async function confirmSwitch(): Promise<boolean> {
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
      return true
    } catch {
      return false
    }
  }

  // Select a template
  async function selectTemplate(
    tmpl: Template,
    getCurrentContent: () => string,
    isInPreview: boolean,
    onBeforeSwitch: () => void,
    onContentLoad: (content: string) => void,
    onVersionsLoad: () => Promise<void>
  ): Promise<boolean> {
    // Check for unsaved changes
    if (selectedTemplate.value) {
      const currentContent = getCurrentContent()
      if (hasUnsavedChanges(currentContent, isInPreview)) {
        const confirmed = await confirmSwitch()
        if (!confirmed) return false
      }
    }

    loading.value = true
    errorMsg.value = ''
    onBeforeSwitch()

    try {
      const fullTmpl = await getTemplate(tmpl.name)
      selectedTemplate.value = fullTmpl
      originalContent.value = fullTmpl.content || ''
      onContentLoad(originalContent.value)

      // Load versions for the newly selected template
      await onVersionsLoad()
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      errorMsg.value = detail || '加载模板内容失败'
      return false
    } finally {
      loading.value = false
    }
  }

  // Reset selection state
  function reset() {
    selectedTemplate.value = null
    originalContent.value = ''
    errorMsg.value = ''
  }

  return {
    selectedTemplate,
    originalContent,
    loading,
    errorMsg,
    selectTemplate,
    reset
  }
}
