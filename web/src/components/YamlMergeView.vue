<template>
  <div ref="mergeViewEl" class="yaml-merge-view"></div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, shallowRef } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { MergeView } from '@codemirror/merge'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { indentUnit } from '@codemirror/language'
import { EditorState } from '@codemirror/state'

interface Props {
  originalContent: string
  modifiedContent: string
  isDark?: boolean
}

interface Emits {
  (e: 'update:modifiedContent', value: string): void
}

const props = withDefaults(defineProps<Props>(), {
  isDark: false
})

const emit = defineEmits<Emits>()

const mergeViewEl = ref<HTMLElement | null>(null)
const mergeView = shallowRef<MergeView | null>(null)

// Create merge view instance
function createMergeView(original: string, modified: string) {
  if (!mergeViewEl.value) return

  const extensions = [basicSetup, yaml(), indentUnit.of('  ')]

  if (props.isDark) {
    extensions.push(oneDark)
  }

  mergeView.value = new MergeView({
    a: {
      doc: original,
      extensions: [
        ...extensions,
        // Left pane = previewed historical version, read-only by design
        EditorState.readOnly.of(true),
        EditorView.editable.of(false)
      ]
    },
    b: {
      doc: modified,
      extensions: [
        ...extensions,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            emit('update:modifiedContent', update.state.doc.toString())
          }
        })
      ]
    },
    parent: mergeViewEl.value
  })
}

// Watch content changes
watch(
  () => [props.originalContent, props.modifiedContent, props.isDark] as const,
  () => {
    if (!mergeView.value) return

    // Recreate merge view with new content/theme
    mergeView.value.destroy()
    createMergeView(props.originalContent, props.modifiedContent)
  }
)

onMounted(() => {
  createMergeView(props.originalContent, props.modifiedContent)
})

onBeforeUnmount(() => {
  mergeView.value?.destroy()
})
</script>

<style scoped>
.yaml-merge-view {
  height: 100%;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
  overflow: hidden;
}

.yaml-merge-view :deep(.cm-mergeView) {
  height: 100%;
}

.yaml-merge-view :deep(.cm-editor) {
  font-size: 13px;
}

.yaml-merge-view :deep(.cm-scroller) {
  overflow: auto;
}

.yaml-merge-view :deep(.cm-gutters) {
  border-right: 1px solid var(--ph-border);
}

.yaml-merge-view :deep(.cm-mergeViewEditor) {
  height: 100%;
}
</style>
