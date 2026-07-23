import { useBatchJob } from './useBatchJob'

// 批量深度体检(检查动作 4):完整四段体检(出网+稳定性+多地域+解锁)的任务化派发,
// 仅选中节点。后端 batch_exam kind 参数化 full 模式(mode=full,见 issue 0027),
// 与精简批量体检共用端点、不同 runner。进度复用 jobs 轮询(完成 x/N,可取消)。
export function useBatchExam(onDone?: () => void) {
  const job = useBatchJob(
    {
      kind: 'batch_exam',
      key: 'batch_exam',
      startUrl: '/nodes/exam/batch',
      cancelUrl: '/nodes/exam/batch/cancel',
      actionLabel: '深度体检'
    },
    onDone
  )

  // start 固定带 mode=full:批量深度体检 = 完整四段口径(收敛后批量入口只暴露 full)。
  const start = (nodeKeys: string[]) => job.start(nodeKeys, { mode: 'full' })

  return {
    running: job.running,
    completed: job.completed,
    total: job.total,
    start,
    cancel: job.cancel
  }
}
