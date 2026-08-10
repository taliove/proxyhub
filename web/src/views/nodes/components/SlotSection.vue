<template>
  <!-- 名称槽位区(ADR 0047 / issue #98):管理区 + 行内"命名"对话框,
       状态自持,变更后上抛 changed 让页面重取节点池(显示名经服务端槽位层叠加) -->
  <!-- 待关注横幅(issue #103):空槽/挂载节点消失常显,与飞书 push 互补 -->
  <el-alert
    v-if="attentionSlots.length > 0"
    type="warning"
    :closable="false"
    class="slot-banner"
    :title="`有 ${attentionSlots.length} 个名称槽位待处理（空槽或挂载节点已消失），请在下方名称槽位区指派节点`"
  />
  <SlotManager
    :slots="slots"
    :conflicts="conflicts"
    :loading="loading"
    :nodes="nodes"
    @changed="onChanged"
  />
  <NodeSlotAssignDialog ref="assignDialog" :empty-slot-names="emptySlotNames" @saved="onChanged" />
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import SlotManager from './SlotManager.vue'
import NodeSlotAssignDialog from './NodeSlotAssignDialog.vue'
import { useNameSlots } from '../composables/useNameSlots'
import type { UnifiedNode } from '../selfmerge'

defineProps<{ nodes: UnifiedNode[] }>()

const emit = defineEmits<{
  (e: 'changed'): void
}>()

const { slots, conflicts, loading, load, slotNameByNodeKey, emptySlots, attentionSlots } =
  useNameSlots()

const emptySlotNames = computed(() => emptySlots.value.map((s) => s.name))
// 名称列"槽位"标记用的占用集合(供父页传 NodeTable)
const slotKeys = computed(() => new Set(slotNameByNodeKey.value.keys()))

const assignDialog = ref<InstanceType<typeof NodeSlotAssignDialog> | null>(null)
const openAssign = (row: UnifiedNode) =>
  assignDialog.value?.open(row, slotNameByNodeKey.value.get(row.node_key) || '')

const onChanged = async () => {
  await load()
  emit('changed')
}

onMounted(load)

defineExpose({ openAssign, slotKeys, attentionSlots })
</script>

<style scoped>
.slot-banner {
  margin-bottom: var(--ph-space-3);
}
</style>
