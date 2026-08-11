import { ref, type Ref } from 'vue'
import { ElMessage } from 'element-plus'
import { setEndpointStatusNode, setEndpointSlotMode } from '@/api/endpoints'
import { extractErrorDetail } from '@/utils/errors'
import type { Endpoint } from '@/types'

// useEndpointToggles 订阅地址的两个直改直存开关(虚拟状态节点/槽位模式),
// 从 EndpointDetailDrawer 抽取(400 行门禁)。changed 由调用方上抛刷新列表。
export const useEndpointToggles = (endpoint: Ref<Endpoint | null>, changed: () => void) => {
  const statusNodeSaving = ref(false)
  const onStatusNodeChange = async (val: string | number | boolean) => {
    if (!endpoint.value) return
    statusNodeSaving.value = true
    try {
      await setEndpointStatusNode(endpoint.value.id, Boolean(val))
      ElMessage.success(val ? '已开启：订阅第一位将展示节点状态' : '已关闭')
      changed()
    } catch (e) {
      ElMessage.error(extractErrorDetail(e) || '保存失败')
    } finally {
      statusNodeSaving.value = false
    }
  }

  // 槽位模式(节点来源):开启后精选/节点范围不再生效,由名称槽位决定
  const slotModeSaving = ref(false)
  const onSlotModeChange = async (val: string | number | boolean) => {
    if (!endpoint.value) return
    const slots = val === 'slots'
    slotModeSaving.value = true
    try {
      await setEndpointSlotMode(endpoint.value.id, slots)
      ElMessage.success(slots ? '已切到槽位模式：只下发名称槽位挂载的节点' : '已切回池模式')
      changed()
    } catch (e) {
      ElMessage.error(extractErrorDetail(e) || '保存失败')
    } finally {
      slotModeSaving.value = false
    }
  }

  return { statusNodeSaving, onStatusNodeChange, slotModeSaving, onSlotModeChange }
}
