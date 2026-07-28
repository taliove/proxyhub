import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTemplateAutosave } from '../useTemplateAutosave'
import * as templatesApi from '@/api/templates'

// Mock the API module
vi.mock('@/api/templates', () => ({
  updateTemplate: vi.fn()
}))

describe('useTemplateAutosave', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('baseline sync on autosave success', () => {
    it('should invoke onSaved callback with persisted content on successful autosave', async () => {
      const autosave = useTemplateAutosave()
      const contentRef = ref('key: value1')
      const templateNameGetter = () => 'test-template'
      const canAutosave = () => true
      const onSavedMock = vi.fn()

      // Mock successful API call
      vi.mocked(templatesApi.updateTemplate).mockResolvedValue({ success: true })

      // Setup autosave with onSaved callback
      autosave.setupAutosave(contentRef, templateNameGetter, canAutosave, onSavedMock)

      // Change content to trigger autosave
      contentRef.value = 'key: value2'
      await nextTick()

      // Fast-forward past debounce
      vi.advanceTimersByTime(1000)
      await vi.runAllTimersAsync()

      // Verify onSaved was called with the persisted content
      expect(onSavedMock).toHaveBeenCalledTimes(1)
      expect(onSavedMock).toHaveBeenCalledWith('key: value2')
      expect(autosave.lastSavedAt.value).not.toBe('')
    })

    it('should not invoke onSaved callback when validation fails', async () => {
      const autosave = useTemplateAutosave()
      const contentRef = ref('invalid: [yaml')
      const templateNameGetter = () => 'test-template'
      const canAutosave = () => true
      const onSavedMock = vi.fn()

      autosave.setupAutosave(contentRef, templateNameGetter, canAutosave, onSavedMock)

      // Trigger autosave with invalid YAML
      contentRef.value = 'invalid: [unclosed'
      await nextTick()

      vi.advanceTimersByTime(1000)
      await vi.runAllTimersAsync()

      // Verify onSaved was NOT called
      expect(onSavedMock).not.toHaveBeenCalled()
      expect(autosave.validationError.value).toContain('YAML 格式错误')
    })

    it('should not invoke onSaved callback when API call fails', async () => {
      const autosave = useTemplateAutosave()
      const contentRef = ref('key: value')
      const templateNameGetter = () => 'test-template'
      const canAutosave = () => true
      const onSavedMock = vi.fn()

      // Mock API failure
      vi.mocked(templatesApi.updateTemplate).mockRejectedValue(new Error('Network error'))

      autosave.setupAutosave(contentRef, templateNameGetter, canAutosave, onSavedMock)

      contentRef.value = 'key: updated'
      await nextTick()

      vi.advanceTimersByTime(1000)
      await vi.runAllTimersAsync()

      // Verify onSaved was NOT called
      expect(onSavedMock).not.toHaveBeenCalled()
      expect(autosave.validationError.value).not.toBe('')
    })

    it('should use snapshot of content at save time, not callback invocation time', async () => {
      const autosave = useTemplateAutosave()
      const contentRef = ref('initial')
      const templateNameGetter = () => 'test-template'
      const canAutosave = () => true
      const onSavedMock = vi.fn()

      // Mock API call that resolves after a delay
      vi.mocked(templatesApi.updateTemplate).mockImplementation(() => {
        return new Promise((resolve) => {
          setTimeout(() => resolve({ success: true }), 100)
        })
      })

      autosave.setupAutosave(contentRef, templateNameGetter, canAutosave, onSavedMock)

      // Change content to trigger autosave
      contentRef.value = 'saved-content'
      await nextTick()

      // Fast-forward to trigger debounce (autosave starts)
      vi.advanceTimersByTime(1000)
      await nextTick()

      // User continues typing while save is in flight
      contentRef.value = 'saved-content-plus-more-typing'
      await nextTick()

      // Complete the API call
      vi.advanceTimersByTime(100)
      await vi.runAllTimersAsync()

      // onSaved should be called with the snapshot at save time (saved-content),
      // NOT the current editor content (saved-content-plus-more-typing)
      expect(onSavedMock).toHaveBeenCalledWith('saved-content')
    })
  })

  describe('existing validation and autosave logic', () => {
    it('should validate empty content', () => {
      const autosave = useTemplateAutosave()
      const result = autosave.validateYaml('')
      expect(result.valid).toBe(false)
      expect(result.error).toContain('不能为空')
    })

    it('should validate invalid YAML', () => {
      const autosave = useTemplateAutosave()
      const result = autosave.validateYaml('invalid: [unclosed')
      expect(result.valid).toBe(false)
      expect(result.error).toContain('YAML 格式错误')
    })

    it('should validate valid YAML', () => {
      const autosave = useTemplateAutosave()
      const result = autosave.validateYaml('key: value\nlist:\n  - item1\n  - item2')
      expect(result.valid).toBe(true)
      expect(result.error).toBeUndefined()
    })

    it('should not trigger autosave when canAutosave returns false', async () => {
      const autosave = useTemplateAutosave()
      const contentRef = ref('key: value')
      const templateNameGetter = () => 'test-template'
      const canAutosave = () => false
      const onSavedMock = vi.fn()

      autosave.setupAutosave(contentRef, templateNameGetter, canAutosave, onSavedMock)

      contentRef.value = 'key: updated'
      await nextTick()

      vi.advanceTimersByTime(1000)
      await vi.runAllTimersAsync()

      expect(templatesApi.updateTemplate).not.toHaveBeenCalled()
      expect(onSavedMock).not.toHaveBeenCalled()
    })
  })
})
