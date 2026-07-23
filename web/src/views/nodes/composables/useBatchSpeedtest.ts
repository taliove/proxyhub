import { useBatchJob } from './useBatchJob'

// 批量"快速测速"(检查动作 3):逐节点测基准下行(同体检基准行口径)。
// 后端 batch_speedtest kind(issue 0029),可用判定按 down+up 双阈值口径对齐(cd50791)。
// 进度复用 jobs 轮询(完成 x/N,可取消)。
export function useBatchSpeedtest(onDone?: () => void) {
  return useBatchJob(
    {
      kind: 'batch_speedtest',
      key: 'batch_speedtest',
      startUrl: '/nodes/speedtest/batch',
      cancelUrl: '/nodes/speedtest/batch/cancel',
      actionLabel: '快速测速'
    },
    onDone
  )
}
