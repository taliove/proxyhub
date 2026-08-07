import { computed, type Ref } from 'vue'
import type { BatchAction, BatchActionId } from '../components/NodeBatchBar.vue'
import type { UnifiedNode } from '../selfmerge'
import { useNodeDetection, type DetectionScope } from './useNodeDetection'
import { useBatchExam } from './useBatchExam'
import { useBatchStability } from './useBatchStability'
import { useBatchSpeedtest } from './useBatchSpeedtest'

// useNodeBatchActions 统一 4 个检查动作的编排(见 CONTEXT「检查动作」):
//   1 出网快速检测 = 解锁检测(batch_detection,含解锁落库 + 重算标签),全局单例;
//   2 出网+稳定性、3 快速测速、4 深度体检 = 三个同构批量任务(jobs 轮询进度)。
// 单节点与批量共用同一套启动器(单节点即 node_keys 只含本节点),保证口径一致。
// detecting 全页共享(页头告警条 / 批量栏 / 行内下拉 / 清理弹窗都消费),故在此单点持有。
export function useNodeBatchActions(
  selection: Ref<UnifiedNode[]>,
  reload: () => Promise<void> | void,
  reloadExam: () => void
) {
  const {
    running: detecting,
    completed: detectCompleted,
    total: detectTotal,
    trigger,
    cancel
  } = useNodeDetection()

  const selectedKeys = () => selection.value.map((n) => n.node_key)

  // 动作 1:解锁检测,完成后刷新数据与体检摘要(含自建,后端按 node_key 在池中匹配)。
  const detect = (scope: DetectionScope) =>
    trigger(scope, async () => {
      await reload()
      reloadExam()
    })
  const detectSelected = () => detect({ type: 'selected', node_keys: selectedKeys() })
  const detectOne = (node: UnifiedNode) => detect({ type: 'selected', node_keys: [node.node_key] })
  const cancelDetection = () => cancel(reload)
  const triggerCleanupDetection = (onComplete: () => void) =>
    trigger({ type: 'all' }, onComplete, '检测已启动，完成后自动刷新失败列表')

  // 动作 2/3/4:同构批量任务,完成后刷新体检摘要(测速另需 reload 以刷带宽列)。
  const batchStability = useBatchStability(reloadExam)
  const batchSpeedtest = useBatchSpeedtest(() => {
    reload()
    reloadExam()
  })
  const batchExam = useBatchExam(reloadExam)

  // 批量栏渲染描述(顺序即 CONTEXT「检查动作」顺序);label 单点定义,进度实时。
  const batchActions = computed<BatchAction[]>(() => [
    {
      id: 'detect',
      label: '出网快速检测',
      state: {
        running: detecting.value,
        completed: detectCompleted.value,
        total: detectTotal.value
      }
    },
    {
      id: 'stability',
      label: '出网+稳定性',
      state: {
        running: batchStability.running.value,
        completed: batchStability.completed.value,
        total: batchStability.total.value
      }
    },
    {
      id: 'speedtest',
      label: '快速测速',
      state: {
        running: batchSpeedtest.running.value,
        completed: batchSpeedtest.completed.value,
        total: batchSpeedtest.total.value
      }
    },
    {
      id: 'exam',
      label: '深度体检',
      state: {
        running: batchExam.running.value,
        completed: batchExam.completed.value,
        total: batchExam.total.value
      }
    }
  ])

  // 批量分发:统一从选中集取作用域,交由对应任务启动器/取消。
  const onBatchStart = (id: BatchActionId) => {
    const keys = selectedKeys()
    if (id === 'detect') detectSelected()
    else if (id === 'stability') batchStability.start(keys)
    else if (id === 'speedtest') batchSpeedtest.start(keys)
    else if (id === 'exam') batchExam.start(keys)
  }
  const onBatchCancel = (id: BatchActionId) => {
    if (id === 'detect') cancelDetection()
    else if (id === 'stability') batchStability.cancel()
    else if (id === 'speedtest') batchSpeedtest.cancel()
    else if (id === 'exam') batchExam.cancel()
  }

  return {
    detecting,
    detectOne,
    cancelDetection,
    triggerCleanupDetection,
    batchActions,
    onBatchStart,
    onBatchCancel
  }
}
