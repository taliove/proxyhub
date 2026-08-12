import { useBatchJob } from './useBatchJob'

// 批量体检任务(检查动作主入口「补齐信息」+ 高级「深度体检」):
// 共用 batch_exam kind,mode 选 runner——backfill(默认,出网+解锁+短采样稳定性,
// 跳多地域/基准,尽快填满列表字段)| full(完整四段,与单节点深度体检同口径)。
// 进度复用 jobs 轮询(完成 x/N,可取消)。
export function useBatchExam(onDone?: () => void) {
  const job = useBatchJob(
    {
      kind: 'batch_exam',
      key: 'batch_exam',
      startUrl: '/nodes/exam/batch',
      cancelUrl: '/nodes/exam/batch/cancel',
      actionLabel: '补齐信息'
    },
    onDone
  )

  const start = (nodeKeys: string[], mode: 'backfill' | 'full' = 'backfill') =>
    job.start(nodeKeys, { mode }, mode === 'full' ? '深度体检' : undefined)

  return {
    running: job.running,
    completed: job.completed,
    total: job.total,
    start,
    cancel: job.cancel
  }
}
