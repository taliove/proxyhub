import { computed, type Ref } from 'vue'
import type { BatchAction, BatchActionId } from '../components/NodeBatchBar.vue'
import type { UnifiedNode } from '../selfmerge'
import { useNodeDetection, type DetectionScope } from './useNodeDetection'
import { useBatchExam } from './useBatchExam'
import { useBatchStability } from './useBatchStability'
import { useBatchSpeedtest } from './useBatchSpeedtest'

// useNodeBatchActions 统一检查动作的编排(见 CONTEXT「检查动作」):
//   主入口 补齐信息 = batch_exam mode=backfill(出网+解锁+短稳定性,跳多地域/基准);
//   高级下拉:出网快速检测(batch_detection,全局单例)/ 出网+稳定性 / 快速测速 /
//   深度体检(batch_exam mode=full)。
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

  // 动作 2/3/4:同构批量任务,完成后刷新列表与体检摘要
  // (backfill 写回 node_health 与内存池,必须 reload 才能看到延迟/可用性/解锁列)。
  const batchStability = useBatchStability(reloadExam)
  const batchSpeedtest = useBatchSpeedtest(() => {
    reload()
    reloadExam()
  })
  const batchExam = useBatchExam(async () => {
    await reload()
    reloadExam()
  })

  // 单节点补齐信息:与批量同走 batch_exam backfill(单键),完成后自动刷新。
  const backfillOne = (node: UnifiedNode) => batchExam.start([node.node_key], 'backfill')

  // 批量栏渲染描述(顺序即 CONTEXT「检查动作」顺序);label 单点定义,进度实时。
  // exam = 主入口「补齐信息」(backfill);exam-full = 高级「深度体检」(完整四段)。
  const batchActions = computed<BatchAction[]>(() => [
    {
      id: 'exam',
      label: '补齐信息',
      state: {
        running: batchExam.running.value,
        completed: batchExam.completed.value,
        total: batchExam.total.value
      }
    },
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
      id: 'exam-full',
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
    if (id === 'exam') batchExam.start(keys, 'backfill')
    else if (id === 'exam-full') batchExam.start(keys, 'full')
    else if (id === 'detect') detectSelected()
    else if (id === 'stability') batchStability.start(keys)
    else if (id === 'speedtest') batchSpeedtest.start(keys)
  }
  const onBatchCancel = (id: BatchActionId) => {
    if (id === 'exam' || id === 'exam-full') batchExam.cancel()
    else if (id === 'detect') cancelDetection()
    else if (id === 'stability') batchStability.cancel()
    else if (id === 'speedtest') batchSpeedtest.cancel()
  }

  return {
    detecting,
    detectOne,
    backfillOne,
    cancelDetection,
    triggerCleanupDetection,
    batchActions,
    onBatchStart,
    onBatchCancel
  }
}
