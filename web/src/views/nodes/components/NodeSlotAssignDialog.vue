<template>
  <el-dialog v-model="visible" title="指派名称" width="440px">
    <el-form label-width="90px">
      <el-form-item label="节点">
        <span class="muted">{{ node?.display_name || node?.name || '—' }}</span>
      </el-form-item>
      <el-form-item v-if="currentSlot" label="当前名称">
        <el-tag size="small" type="success" effect="plain">{{ currentSlot.name }}</el-tag>
        <span class="muted hint-inline">保存新名称后，旧名称变空槽</span>
      </el-form-item>
      <el-form-item v-if="emptySlots.length" label="指派方式">
        <el-radio-group v-model="mode" size="small">
          <el-radio-button label="existing">选择空槽</el-radio-button>
          <el-radio-button label="new">新建名称</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="mode === 'existing' && emptySlots.length" label="空槽">
        <el-select v-model="pickedEmptyID" placeholder="选择待指派的空槽名称" class="ctl-full">
          <el-option v-for="s in emptySlots" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
      </el-form-item>
      <el-form-item v-else label="名称">
        <SlotNameEditor v-model="name" :node-key="node?.node_key" placeholder="输入新名称" />
      </el-form-item>
      <el-alert
        v-if="mode === 'existing'"
        type="info"
        :closable="false"
        title="名称立即生效，所有订阅地址统一使用；节点不可用时可把名称转移给别的节点。"
      />
    </el-form>
    <template #footer>
      <el-button v-if="currentSlot" type="warning" plain @click="unassign">摘下名称</el-button>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="!effectiveName" @click="save(false)">
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createSlot,
  readSlotConflict,
  updateSlot,
  type NameSlot,
  type SlotConflict
} from '@/api/slots'
import SlotNameEditor from './SlotNameEditor.vue'
import { apiErrorMessage, slotNameTakenMessage } from '../utils'
import type { UnifiedNode } from '../selfmerge'

const props = defineProps<{ emptySlots: NameSlot[] }>()

const emit = defineEmits<{
  (e: 'saved'): void
}>()

const visible = ref(false)
const saving = ref(false)
const node = ref<UnifiedNode | null>(null)
const name = ref('')
// 当前占用本节点的槽位(按 ID 寻址,issue #112);null = 未占槽
const currentSlot = ref<NameSlot | null>(null)
// 有空槽时默认选空槽,否则直接新建
const mode = ref<'existing' | 'new'>('new')
const pickedEmptyID = ref<number | ''>('')

// 选中的空槽对象(existing 模式)
const pickedSlot = computed(
  () => props.emptySlots.find((s) => s.id === pickedEmptyID.value) ?? null
)

// 生效的目标名:选空槽模式取选择值,新建模式取编辑器输入
const effectiveName = computed(() =>
  mode.value === 'existing' && props.emptySlots.length ? (pickedSlot.value?.name ?? '') : name.value
)

const open = (row: UnifiedNode, occupyingSlot: NameSlot | null = null) => {
  node.value = row
  currentSlot.value = occupyingSlot
  name.value = ''
  pickedEmptyID.value = ''
  mode.value = props.emptySlots.length ? 'existing' : 'new'
  visible.value = true
}

// 冲突确认文案(409 载荷驱动):节点已占别的槽位 / 名字挂在别的节点上
const confirmText = (c: SlotConflict, target: string): string => {
  if (c.kind === 'node_occupied') {
    return `该节点当前挂在名称「${c.holder_name}」上。改挂到「${target}」后，「${c.holder_name}」将变空槽。确认？`
  }
  if (c.kind === 'reassign') {
    return `名称「${target}」当前挂在节点 ${c.holder_node_key} 上，确认转移到本节点？`
  }
  return ''
}

const save = async (force: boolean) => {
  if (!node.value || !effectiveName.value) return
  saving.value = true
  try {
    const target = effectiveName.value
    // 选中已有空槽(或新建名恰好命中空槽):按槽位 ID 指派
    // (空槽无 reassign 冲突;node_occupied 走确认)
    // {index} 模板放行后同名空槽可有多条(issue #113):取 ID 最小者,
    // 与编号排序的创建顺序 tiebreak 口径一致
    const existing =
      mode.value === 'existing'
        ? pickedSlot.value
        : (props.emptySlots.filter((s) => s.name === target).sort((a, b) => a.id - b.id)[0] ?? null)
    if (existing) {
      await updateSlot(existing.id, { nodeKey: node.value.node_key, force })
    } else {
      await createSlot(target, node.value.node_key, force)
    }
    ElMessage.success('已生效，所有订阅立即使用新名称')
    visible.value = false
    emit('saved')
  } catch (e) {
    const conflict = readSlotConflict(e)
    if (conflict && !force) {
      const text = confirmText(conflict, effectiveName.value)
      if (conflict.kind === 'name_taken') {
        ElMessage.error(slotNameTakenMessage(effectiveName.value))
      } else if (text) {
        try {
          await ElMessageBox.confirm(text, '转移确认', { type: 'warning' })
          await save(true)
        } catch {
          /* 用户取消 */
        }
      } else {
        ElMessage.error('操作冲突，请刷新后重试')
      }
    } else {
      ElMessage.error(apiErrorMessage(e, '保存失败'))
    }
  } finally {
    saving.value = false
  }
}

const unassign = async () => {
  if (!currentSlot.value) return
  try {
    await ElMessageBox.confirm(
      `摘下后「${currentSlot.value.name}」变空槽，该节点回退模板/原始名称。确认？`,
      '摘下名称',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await updateSlot(currentSlot.value.id, { nodeKey: '' })
    ElMessage.success('已摘下，名称保留为空槽')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '摘下失败'))
  }
}

defineExpose({ open })
</script>

<style scoped>
.ctl-full {
  width: 100%;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.hint-inline {
  margin-left: var(--ph-space-2);
}
</style>
