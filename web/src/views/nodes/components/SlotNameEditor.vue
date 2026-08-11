<template>
  <div class="slot-name-editor">
    <el-input
      ref="inputRef"
      :model-value="modelValue"
      :placeholder="placeholder"
      clearable
      @input="(v: string) => emit('update:modelValue', v)"
    >
      <template v-if="resolved" #append>
        <span class="preview-inline" :title="`订阅里实际显示：${preview}`">{{ preview }}</span>
      </template>
    </el-input>

    <!-- 变量点击插入(光标处);hover 显示含义 -->
    <div class="var-chips">
      <el-tooltip v-for="v in VARIABLES" :key="v.token" :content="v.desc" placement="top">
        <el-tag size="small" class="var-chip" @click="insert(v.token)">{{ v.token }}</el-tag>
      </el-tooltip>
    </div>

    <!-- 实时预览(含变量时):渲染自挂载节点,与订阅下发同源 -->
    <div v-if="hasVar" class="preview-line">
      <template v-if="resolved">
        实际显示：<strong>{{ preview }}</strong>
      </template>
      <span v-else class="muted">{{ previewHint }}</span>
    </div>

    <!-- 使用案例:常显,点击直接填入输入框 -->
    <div class="examples">
      <div class="examples-title">使用案例（点击填入）</div>
      <div
        v-for="ex in EXAMPLES"
        :key="ex.tpl"
        class="example-row"
        :title="`点击填入 ${ex.tpl}`"
        @click="fill(ex.tpl)"
      >
        <code>{{ ex.tpl }}</code> → {{ ex.desc }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { previewSlotName } from '@/api/slots'

// SlotNameEditor 槽位名编辑器:变量点击插入 + 挂载节点实时预览 + 可点填案例。
// 预览走后端渲染(与订阅生成同一 Standardizer),不做前端近似。
const props = defineProps<{
  modelValue: string
  // 预览渲染所针对的节点(指派=目标节点,改名=当前挂载节点);空 = 无上下文,不预览
  nodeKey?: string
  placeholder?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const VARIABLES = [
  { token: '{emoji}', desc: '地区国旗，如 🇭🇰' },
  { token: '{region}', desc: '地区中文名，如 香港' },
  { token: '{region_code}', desc: '地区码，如 HK' },
  { token: '{source}', desc: '来源机场全名' },
  { token: '{source_abbr}', desc: '机场简称' },
  { token: '{original_name}', desc: '机场原始节点名' },
  { token: '{index}', desc: '序号：渲染前缀相同的槽位自动编号（01、02…）' }
] as const

const EXAMPLES = [
  { tpl: '主节点', desc: '固定名，换节点不变' },
  {
    tpl: '主节点-{region}',
    desc: '跟随地区：挂香港显示「主节点-香港」，转美国自动变「主节点-美国」'
  },
  {
    tpl: '{emoji}-主节点-{region}-{index}',
    desc: '带国旗+序号：同前缀槽位自动编号，如「🇭🇰-主节点-香港-01」'
  },
  { tpl: '备-{original_name}', desc: '保留机场原名后缀，如「备-HK-01」' }
] as const

const inputRef = ref<{ focus: () => void; input?: HTMLInputElement } | null>(null)

// 点击变量插入到光标处(无光标信息时追加到末尾),插入后保持焦点
const insert = (token: string) => {
  const el = inputRef.value?.input
  const cur = props.modelValue || ''
  if (el && typeof el.selectionStart === 'number') {
    const pos = el.selectionStart
    emit('update:modelValue', cur.slice(0, pos) + token + cur.slice(el.selectionEnd ?? pos))
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(pos + token.length, pos + token.length)
    })
    return
  }
  emit('update:modelValue', cur + token)
}

// 案例点填:整名替换并聚焦
const fill = (tpl: string) => {
  emit('update:modelValue', tpl)
  requestAnimationFrame(() => inputRef.value?.focus())
}

const hasVar = computed(() => (props.modelValue || '').includes('{'))

// 实时预览:300ms 防抖,只打一次后端
const preview = ref('')
const resolved = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => [props.modelValue, props.nodeKey],
  () => {
    clearTimeout(timer)
    if (!hasVar.value || !props.nodeKey) {
      resolved.value = false
      return
    }
    timer = setTimeout(async () => {
      try {
        const resp = await previewSlotName(props.modelValue, props.nodeKey!)
        preview.value = resp.rendered
        resolved.value = resp.resolved
      } catch {
        resolved.value = false // 预览失败不阻塞输入
      }
    }, 300)
  },
  { immediate: true }
)

const previewHint = computed(() =>
  props.nodeKey ? '输入变量后显示实际名称预览' : '空槽暂无挂载节点，指派后可预览实际名称'
)
</script>

<style scoped>
.slot-name-editor {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-2);
}
.var-chips {
  display: flex;
  gap: var(--ph-space-1);
  flex-wrap: wrap;
}
.var-chip {
  cursor: pointer;
  font-family: var(--ph-font-mono, monospace);
}
.preview-inline {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ph-text-secondary);
}
.preview-line {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.preview-line strong {
  color: var(--ph-text-primary);
}
.examples {
  border-top: 1px dashed var(--ph-border);
  padding-top: var(--ph-space-2);
}
.examples-title {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-bottom: var(--ph-space-1);
}
.example-row {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.9;
  cursor: pointer;
}
.example-row:hover {
  color: var(--ph-text-primary);
}
.example-row code {
  color: var(--ph-text-primary);
}
</style>
