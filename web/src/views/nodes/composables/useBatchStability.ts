import { useBatchJob } from './useBatchJob'

// 批量"出网+稳定性"(检查动作 2):两阶段出网画像 + 稳定性评分,不含解锁/测速。
// 后端 batch_stability kind(issue 0028),结果带 source=stability_check 落 exam_history,
// 不抢占"最近体检"单一事实源。进度复用 jobs 轮询(完成 x/N,可取消)。
export function useBatchStability(onDone?: () => void) {
  return useBatchJob(
    {
      kind: 'batch_stability',
      key: 'batch_stability',
      startUrl: '/nodes/stability/batch',
      cancelUrl: '/nodes/stability/batch/cancel',
      actionLabel: '出网+稳定性检查'
    },
    onDone
  )
}
