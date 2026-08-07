// 实测运行的状态机:封装 runner,对外暴露阶段、实时速率、定格值、延迟/抖动、最终结果与取消。
// 纯状态管理,不碰 DOM;落库与历史刷新由调用侧(页面)在 onDone 后串接。
// nodeKey 由调用侧传入(空串 = 直连基线),runner 据此订阅 SSE 流。
import { ref, shallowRef } from 'vue'
import { runSpeedtest, type SpeedtestOutcome, type SpeedtestPhase } from '../runner'

export function useSpeedtestRun() {
  // phase: null = 空闲;result: 最近一次完成的产出
  const phase = ref<SpeedtestPhase | null>(null)
  const running = ref(false)
  const liveMbps = ref(0) // 当前阶段实时速率（下行/上行大数字实时刷新）
  // downFinal/upFinal:该方向测完后的定格值(最后 sample),用于上行阶段仍显示下行结果、
  // done 前的过渡展示,避免阶段切换时下行卡片变横杠。
  const downFinalMbps = ref(0)
  const upFinalMbps = ref(0)
  const idleLatencyMs = ref(0) // 延迟阶段测出的空闲延迟（latency 帧即有值，不必等 done）
  const jitterMs = ref(0)
  const result = shallowRef<SpeedtestOutcome | null>(null)
  const error = ref('')
  let controller: AbortController | null = null

  const start = async (nodeKey: string): Promise<SpeedtestOutcome | null> => {
    if (running.value) return null
    running.value = true
    error.value = ''
    liveMbps.value = 0
    downFinalMbps.value = 0
    upFinalMbps.value = 0
    idleLatencyMs.value = 0
    jitterMs.value = 0
    phase.value = null
    result.value = null
    controller = new AbortController()
    try {
      const outcome = await runSpeedtest(
        nodeKey,
        {
          onLatency: (lat, jit) => {
            idleLatencyMs.value = lat
            jitterMs.value = jit
          },
          onPhase: (p: SpeedtestPhase) => {
            phase.value = p
            liveMbps.value = 0 // 阶段切换时重置实时数字（downFinal/upFinal 保留定格）
          },
          onSample: (p: SpeedtestPhase, mbps: number) => {
            liveMbps.value = mbps
            if (p === 'download') downFinalMbps.value = mbps
            else if (p === 'upload') upFinalMbps.value = mbps
          }
        },
        controller.signal
      )
      result.value = outcome
      return outcome
    } catch (err) {
      if (!controller.signal.aborted) {
        error.value = err instanceof Error ? err.message : String(err)
      }
      return null
    } finally {
      running.value = false
      phase.value = null
      controller = null
    }
  }

  const cancel = () => controller?.abort()

  return {
    phase,
    running,
    liveMbps,
    downFinalMbps,
    upFinalMbps,
    idleLatencyMs,
    jitterMs,
    result,
    error,
    start,
    cancel
  }
}
