import { ref, computed, type Ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Endpoint } from '@/types'
import { resetEndpointLink, extendEndpointGrace } from '@/api/endpoints'

// useEndpointResetLink 订阅链接重置逻辑(issue #117):
// 二次确认 → 原位轮换 → 成功弹窗(EndpointResetResultDialog);
// 宽限延长 +3 天;全部重置逐条调用(服务端无批量端点)。
// 哑组件 Endpoints.vue 只负责接线与刷新。
export function useEndpointResetLink(
  endpoints: Ref<Endpoint[]>,
  getSubscriptionUrl: (row: Endpoint) => string,
  reload: () => Promise<void>
) {
  const resetSuccessVisible = ref(false)
  const resetResult = ref<Endpoint | null>(null)
  const resetNewUrl = computed(() =>
    resetResult.value ? getSubscriptionUrl(resetResult.value) : ''
  )

  const resetLink = async (row: Endpoint) => {
    await ElMessageBox.confirm(
      `重置订阅地址「${row.alias}」的链接后,旧链接进入 3 天宽限期,宽限期后所有设备上的旧订阅将无法更新,需要重新导入新链接。端点上的筛选、精选、模板等配置全部保留。`,
      '重置订阅链接',
      { type: 'warning', confirmButtonText: '确认重置', cancelButtonText: '取消' }
    )
    try {
      resetResult.value = await resetEndpointLink(row.id)
      resetSuccessVisible.value = true
      reload()
    } catch (err) {
      ElMessage.error(`重置失败:${err instanceof Error ? err.message : String(err)}`)
    }
  }

  const extendGrace = async (row: Endpoint) => {
    try {
      await extendEndpointGrace(row.id)
      ElMessage.success('宽限期已延长 3 天')
      reload()
    } catch (err) {
      ElMessage.error(`延长失败:${err instanceof Error ? err.message : String(err)}`)
    }
  }

  const resetAllLoading = ref(false)
  const resetAllLinks = async () => {
    await ElMessageBox.confirm(
      `即将重置全部 ${endpoints.value.length} 条订阅地址的链接。所有旧链接进入 3 天宽限期,宽限期后全部设备上的旧订阅将无法更新。端点配置全部保留。`,
      '全部重置链接',
      { type: 'warning', confirmButtonText: '全部重置', cancelButtonText: '取消' }
    )
    resetAllLoading.value = true
    let ok = 0
    const failed: string[] = []
    for (const ep of endpoints.value) {
      try {
        await resetEndpointLink(ep.id)
        ok++
      } catch {
        failed.push(ep.alias)
      }
    }
    resetAllLoading.value = false
    if (failed.length === 0) {
      ElMessage.success(`已全部重置(${ok} 条)`)
    } else {
      ElMessage.error(`成功 ${ok} 条,失败 ${failed.length} 条:${failed.join('、')}`)
    }
    reload()
  }

  return {
    resetSuccessVisible,
    resetResult,
    resetNewUrl,
    resetLink,
    extendGrace,
    resetAllLoading,
    resetAllLinks
  }
}
