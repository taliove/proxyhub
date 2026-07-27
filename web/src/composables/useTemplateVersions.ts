// Template version history composable
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listTemplateVersions, getTemplateVersion, type TemplateVersion } from '@/api/templates'
import { extractErrorDetail } from '@/utils/errors'

export function useTemplateVersions() {
  const versions = ref<TemplateVersion[]>([])
  const versionsLoading = ref(false)
  const previewingVersion = ref<number | null>(null)
  const currentVersion = ref<number | null>(null)
  const restoring = ref(false)

  // Load version history
  async function loadVersions(templateName: string) {
    versionsLoading.value = true
    try {
      const data = await listTemplateVersions(templateName)
      versions.value = data.versions

      // The first version in descending order is the current version
      if (versions.value.length > 0) {
        currentVersion.value = versions.value[0].version
      }
    } catch (e) {
      console.error('Failed to load versions:', e)
    } finally {
      versionsLoading.value = false
    }
  }

  // Preview a specific version
  async function previewVersion(
    templateName: string,
    version: number,
    onContentLoad: (content: string) => void
  ) {
    try {
      const versionData = await getTemplateVersion(templateName, version)
      previewingVersion.value = version
      onContentLoad(versionData.content)
    } catch (e) {
      const detail = extractErrorDetail(e)
      ElMessage.error(detail || '加载版本内容失败')
    }
  }

  // Exit preview mode
  function exitPreview(originalContent: string, onContentLoad: (content: string) => void) {
    previewingVersion.value = null
    onContentLoad(originalContent)
  }

  // Restore a version (save it as new current version)
  async function restoreVersion(
    templateName: string,
    version: number,
    contentToRestore: string,
    onSuccess: () => void,
    validateAndSave: (content: string) => Promise<boolean>
  ) {
    try {
      await ElMessageBox.confirm(
        `确定恢复到版本 ${version} 吗？恢复后将作为新版本保存。`,
        '恢复版本',
        {
          type: 'warning',
          confirmButtonText: '恢复',
          cancelButtonText: '取消'
        }
      )
    } catch {
      return
    }

    restoring.value = true
    try {
      const success = await validateAndSave(contentToRestore)
      if (success) {
        previewingVersion.value = null
        ElMessage.success('版本已恢复')
        await loadVersions(templateName)
        onSuccess()
      }
    } finally {
      restoring.value = false
    }
  }

  // Reset state (for template switching)
  function reset() {
    versions.value = []
    previewingVersion.value = null
    currentVersion.value = null
    restoring.value = false
  }

  return {
    versions,
    versionsLoading,
    previewingVersion,
    currentVersion,
    restoring,
    loadVersions,
    previewVersion,
    exitPreview,
    restoreVersion,
    reset
  }
}
