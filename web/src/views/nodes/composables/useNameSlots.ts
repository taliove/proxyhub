import { computed, ref } from 'vue'
import { listSlots, type NameSlot, type SlotConflictRow } from '@/api/slots'

// useNameSlots 名称槽位状态(issue #98):节点管理页共享——槽位管理区、
// 行内"命名"入口、名称列槽位标记同读一份,任一操作后 load() 重取。
export const useNameSlots = () => {
  const slots = ref<NameSlot[]>([])
  const conflicts = ref<SlotConflictRow[]>([])
  const loading = ref(false)
  // 监控总开关(issue #103):关时探测列显示"监控未开启"指引而非横杠
  const monitorEnabled = ref(false)

  const load = async () => {
    loading.value = true
    try {
      const resp = await listSlots()
      slots.value = resp.slots || []
      conflicts.value = resp.conflicts || []
      monitorEnabled.value = resp.monitor_enabled === true
    } finally {
      loading.value = false
    }
  }

  // node_key → 槽位(变更操作按 ID 寻址,issue #112;展示层用 slotNameByNodeKey)
  const slotByNodeKey = computed(() => {
    const m = new Map<string, NameSlot>()
    for (const s of slots.value) {
      if (s.node_key) m.set(s.node_key, s)
    }
    return m
  })

  // node_key → 槽位名(名称列标记 + 行内入口判断"此节点已占槽位")
  const slotNameByNodeKey = computed(() => {
    const m = new Map<string, string>()
    for (const [key, s] of slotByNodeKey.value) {
      m.set(key, s.name)
    }
    return m
  })

  // 空槽(可被指派的名字)
  const emptySlots = computed(() => slots.value.filter((s) => s.empty))

  // 待关注槽位:空槽或挂载节点已消失/下架(管理区亮灯 + 后续横幅复用)
  const attentionSlots = computed(() =>
    slots.value.filter((s) => s.empty || s.node?.stale || s.node?.missing)
  )

  return {
    slots,
    conflicts,
    loading,
    monitorEnabled,
    load,
    slotByNodeKey,
    slotNameByNodeKey,
    emptySlots,
    attentionSlots
  }
}
