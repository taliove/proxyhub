// Autosave composable with YAML validation gate
import { ref, watch, onBeforeUnmount } from 'vue'
import * as yaml from 'js-yaml'
import { updateTemplate } from '@/api/templates'
import { extractErrorDetail } from '@/utils/errors'

const AUTOSAVE_DEBOUNCE_MS = 1000

export function useTemplateAutosave() {
  const autosaving = ref(false)
  const validationError = ref('')
  const lastSavedAt = ref('')
  let autosaveTimer: ReturnType<typeof setTimeout> | null = null

  // Validate YAML content
  function validateYaml(content: string): { valid: boolean; error?: string } {
    if (!content.trim()) {
      return { valid: false, error: '模板内容不能为空' }
    }

    try {
      yaml.load(content)
      return { valid: true }
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : String(e)
      return { valid: false, error: `YAML 格式错误: ${errMsg}` }
    }
  }

  // Trigger autosave with validation gate
  async function triggerAutosave(
    templateName: string,
    content: string,
    onSaved?: (savedContent: string) => void
  ) {
    validationError.value = ''

    const validation = validateYaml(content)
    if (!validation.valid) {
      validationError.value = validation.error || '校验失败'
      return
    }

    autosaving.value = true
    try {
      await updateTemplate(templateName, { content })
      lastSavedAt.value = new Date().toISOString()
      if (onSaved) {
        onSaved(content)
      }
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      validationError.value = detail || '保存失败'
      return false
    } finally {
      autosaving.value = false
    }
  }

  // Setup debounced autosave watcher
  function setupAutosave(
    contentRef: { value: string },
    templateNameGetter: () => string | null,
    canAutosave: () => boolean,
    onSaved?: (savedContent: string) => void
  ) {
    watch(
      () => contentRef.value,
      (newContent) => {
        if (!canAutosave()) {
          return
        }

        if (autosaveTimer) {
          clearTimeout(autosaveTimer)
        }

        // Capture snapshot of content at the time autosave is scheduled
        const contentSnapshot = newContent
        autosaveTimer = setTimeout(() => {
          const templateName = templateNameGetter()
          if (templateName) {
            triggerAutosave(templateName, contentSnapshot, onSaved)
          }
        }, AUTOSAVE_DEBOUNCE_MS)
      }
    )
  }

  // Cleanup on unmount
  onBeforeUnmount(() => {
    if (autosaveTimer) {
      clearTimeout(autosaveTimer)
    }
  })

  // Reset state (for template switching)
  function reset() {
    validationError.value = ''
    lastSavedAt.value = ''
    if (autosaveTimer) {
      clearTimeout(autosaveTimer)
      autosaveTimer = null
    }
  }

  return {
    autosaving,
    validationError,
    lastSavedAt,
    validateYaml,
    triggerAutosave,
    setupAutosave,
    reset
  }
}
