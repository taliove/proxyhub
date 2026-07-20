import { ref, type Ref } from 'vue'

/**
 * Composable for debounced auto-suggestion pattern:
 * input event -> debounce -> call suggest function -> write back target if not dirty.
 *
 * Common pattern: user types in field A, after a short delay we call an API
 * to auto-fill field B. Manual edits to field B set a dirty flag to stop
 * auto-filling.
 *
 * @param targetRef - the ref to write suggestion results into
 * @param suggestFn - async function that returns the suggestion value or null/undefined
 * @param debounceMs - debounce delay in milliseconds (default 300)
 * @returns { isDirty, onInput, reset }
 */
export function useDebouncedSuggest<T>(
  targetRef: Ref<T>,
  suggestFn: () => Promise<T | null | undefined>,
  debounceMs = 300
) {
  const isDirty = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null

  /**
   * Call this on input event of the trigger field.
   * If already dirty, does nothing.
   * Otherwise debounces and calls suggestFn, writing result to targetRef if still not dirty.
   */
  const onInput = () => {
    if (isDirty.value) return
    if (timer) clearTimeout(timer)

    timer = setTimeout(async () => {
      try {
        const result = await suggestFn()
        // Double-check dirty flag after async call completes
        // (user might have manually edited during debounce window)
        if (!isDirty.value && result != null) {
          targetRef.value = result
        }
      } catch {
        // API failure: silently skip, let user fill manually
      }
    }, debounceMs)
  }

  /**
   * Call this to reset dirty flag and clear any pending timer.
   * Typically called when opening a dialog or form.
   */
  const reset = () => {
    isDirty.value = false
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  return {
    isDirty,
    onInput,
    reset
  }
}
