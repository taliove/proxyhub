import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useTemplateVersions } from './useTemplateVersions'
import * as templatesApi from '@/api/templates'

// Mock the API module
vi.mock('@/api/templates', () => ({
  listTemplateVersions: vi.fn(),
  getTemplateVersion: vi.fn()
}))

// Mock Element Plus
vi.mock('element-plus', () => ({
  ElMessage: {
    error: vi.fn(),
    success: vi.fn()
  },
  ElMessageBox: {
    confirm: vi.fn()
  }
}))

describe('useTemplateVersions - diff preview', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('previewVersion for diff', () => {
    it('should load historical version content and set preview state', async () => {
      const mockVersionData = {
        version: 2,
        content: 'historical content',
        created_at: '2026-07-28T10:00:00Z'
      }

      vi.mocked(templatesApi.getTemplateVersion).mockResolvedValue(mockVersionData)

      const { previewVersion, previewingVersion } = useTemplateVersions()
      const onContentLoad = vi.fn()

      await previewVersion('test-template', 2, onContentLoad)

      expect(previewingVersion.value).toBe(2)
      expect(onContentLoad).toHaveBeenCalledWith('historical content')
      expect(templatesApi.getTemplateVersion).toHaveBeenCalledWith('test-template', 2)
    })

    it('should handle preview error gracefully', async () => {
      vi.mocked(templatesApi.getTemplateVersion).mockRejectedValue(
        new Error('Network error')
      )

      const { previewVersion, previewingVersion } = useTemplateVersions()
      const onContentLoad = vi.fn()

      await previewVersion('test-template', 2, onContentLoad)

      expect(previewingVersion.value).toBeNull()
      expect(onContentLoad).not.toHaveBeenCalled()
    })
  })

  describe('exitPreview', () => {
    it('should restore original content and clear preview state', () => {
      const { exitPreview, previewingVersion } = useTemplateVersions()

      // Simulate being in preview mode
      previewingVersion.value = 2

      const originalContent = 'current editor content'
      const onContentLoad = vi.fn()

      exitPreview(originalContent, onContentLoad)

      expect(previewingVersion.value).toBeNull()
      expect(onContentLoad).toHaveBeenCalledWith('current editor content')
    })

    it('should preserve unsaved changes when exiting preview', () => {
      const { exitPreview } = useTemplateVersions()

      const unsavedContent = 'current content with unsaved changes'
      const onContentLoad = vi.fn()

      exitPreview(unsavedContent, onContentLoad)

      // Should restore the exact content passed, not lose modifications
      expect(onContentLoad).toHaveBeenCalledWith(unsavedContent)
    })
  })

  describe('diff preview data flow', () => {
    it('should provide correct content for left (historical) and right (current) panes', async () => {
      const mockVersionData = {
        version: 3,
        content: 'version 3 content',
        created_at: '2026-07-28T09:00:00Z'
      }

      vi.mocked(templatesApi.getTemplateVersion).mockResolvedValue(mockVersionData)

      const { previewVersion, previewingVersion } = useTemplateVersions()
      let loadedContent = ''

      await previewVersion('test-template', 3, (content) => {
        loadedContent = content
      })

      // In preview state:
      // - previewingVersion indicates we're in preview mode
      // - loadedContent (passed to callback) should be the historical version
      // - parent component will use this for left pane
      // - parent component's current editor content is used for right pane
      expect(previewingVersion.value).toBe(3)
      expect(loadedContent).toBe('version 3 content')
    })

    it('should maintain preview state across multiple operations', async () => {
      const mockVersionData = {
        version: 5,
        content: 'old content',
        created_at: '2026-07-28T08:00:00Z'
      }

      vi.mocked(templatesApi.getTemplateVersion).mockResolvedValue(mockVersionData)

      const { previewVersion, exitPreview, previewingVersion } = useTemplateVersions()

      // Enter preview
      await previewVersion('test-template', 5, vi.fn())
      expect(previewingVersion.value).toBe(5)

      // Exit preview
      exitPreview('current content', vi.fn())
      expect(previewingVersion.value).toBeNull()

      // Enter preview again
      await previewVersion('test-template', 5, vi.fn())
      expect(previewingVersion.value).toBe(5)
    })
  })
})
