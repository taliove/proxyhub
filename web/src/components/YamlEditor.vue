<template>
  <div ref="editorEl" class="yaml-editor"></div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, shallowRef } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { indentUnit } from '@codemirror/language'

interface Props {
  modelValue: string
  isDark?: boolean
}

interface Emits {
  (e: 'update:modelValue', value: string): void
}

const props = withDefaults(defineProps<Props>(), {
  isDark: false
})

const emit = defineEmits<Emits>()

const editorEl = ref<HTMLElement | null>(null)
const view = shallowRef<EditorView | null>(null)

// Create editor instance
function createEditor(content: string) {
  if (!editorEl.value) return

  const extensions = [
    basicSetup,
    yaml(),
    indentUnit.of('  '),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        emit('update:modelValue', update.state.doc.toString())
      }
    })
  ]

  if (props.isDark) {
    extensions.push(oneDark)
  }

  view.value = new EditorView({
    state: EditorState.create({
      doc: content,
      extensions
    }),
    parent: editorEl.value
  })
}

// Public API for parent component (compatible with Monaco interface)
function getValue(): string {
  return view.value?.state.doc.toString() ?? ''
}

function setValue(content: string): void {
  if (!view.value) return
  view.value.dispatch({
    changes: {
      from: 0,
      to: view.value.state.doc.length,
      insert: content
    }
  })
}

// Watch theme changes
watch(
  () => props.isDark,
  () => {
    if (!view.value) return

    // Recreate editor with new theme
    const currentContent = getValue()
    view.value.destroy()
    createEditor(currentContent)
  }
)

onMounted(() => {
  createEditor(props.modelValue)
})

onBeforeUnmount(() => {
  view.value?.destroy()
})

// Expose API for parent component
defineExpose({
  getValue,
  setValue
})
</script>

<style scoped>
.yaml-editor {
  height: 100%;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
  overflow: hidden;
}

.yaml-editor :deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
}

.yaml-editor :deep(.cm-scroller) {
  overflow: auto;
}

.yaml-editor :deep(.cm-gutters) {
  border-right: 1px solid var(--ph-border);
}
</style>
