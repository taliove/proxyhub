<template>
  <!-- 指派/换人:从节点池选节点挂到该槽位(从 SlotManager 抽取,400 行门禁) -->
  <el-dialog v-model="visible" :title="`指派节点 - ${slot?.name || ''}`" width="480px">
    <el-select v-model="nodeKey" placeholder="搜索节点（名称/地区）" filterable class="ctl-full">
      <el-option
        v-for="n in candidates"
        :key="n.node_key"
        :label="`${n.display_name || n.name} · ${n.region || '—'} · ${n.source}`"
        :value="n.node_key"
      >
        <span>{{ n.display_name || n.name }}</span>
        <span class="muted option-meta">{{ n.region || '—' }} · {{ n.source }}</span>
      </el-option>
    </el-select>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :disabled="!nodeKey" @click="doAssign(false)">确认</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { readSlotConflict, updateSlot, type NameSlot } from '@/api/slots'
import { apiErrorMessage } from '../utils'
import type { UnifiedNode } from '../selfmerge'

const props = defineProps<{
  // 指派候选:统一行集(机场+自建)
  nodes: UnifiedNode[]
}>()

const emit = defineEmits<{
  (e: 'saved'): void
}>()

const visible = ref(false)
const slot = ref<NameSlot | null>(null)
const nodeKey = ref('')

// 可用节点排前,便于挑替补
const candidates = computed(() =>
  [...props.nodes].sort((a, b) => Number(b.available) - Number(a.available))
)

const open = (row: NameSlot) => {
  slot.value = row
  nodeKey.value = ''
  visible.value = true
}

const doAssign = async (force: boolean) => {
  if (!slot.value || !nodeKey.value) return
  const slotID = slot.value.id
  const slotName = slot.value.name
  try {
    await updateSlot(slotID, { nodeKey: nodeKey.value, force })
    ElMessage.success('已生效，所有订阅立即使用新名称')
    visible.value = false
    emit('saved')
  } catch (e) {
    const conflict = readSlotConflict(e)
    if (conflict && !force) {
      let text = ''
      if (conflict.kind === 'node_occupied') {
        text = `该节点当前挂在名称「${conflict.holder_name}」上。改挂到「${slotName}」后，「${conflict.holder_name}」将变空槽。确认？`
      } else if (conflict.kind === 'reassign') {
        text = `名称「${slotName}」当前挂在节点 ${conflict.holder_node_key} 上，确认换人？`
      }
      if (text) {
        try {
          await ElMessageBox.confirm(text, '转移确认', { type: 'warning' })
          await doAssign(true)
        } catch {
          /* 用户取消 */
        }
        return
      }
    }
    ElMessage.error(apiErrorMessage(e, '指派失败'))
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
.option-meta {
  float: right;
}
</style>
