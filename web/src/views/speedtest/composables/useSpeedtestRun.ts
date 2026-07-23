// 实测运行的状态机:封装 runner,对外暴露阶段、实时速率、延迟/抖动、最终结果与取消。
// 纯状态管理,不碰 DOM;落库与历史刷新由调用侧(页面)在 onDone 后串接。
// nodeKey 由调用侧传入(空串 = 直连基线),runner 据此订阅 SSE 流。
import { ref, shallowRef } from 'vue'
import { runSpeedtest, type SpeedtestOutcome, type SpeedtestPhase } from '../runner'

export function useSpeedtestRun() {
  // phase: null = 空闲;result: 最近一次完成的产出
  const phase = ref<SpeedtestPhase | null>(null)
  const running = ref(false)
  const liveMbps = ref(0) // 当前阶段实时速率(下行/上行大数字实时刷新)
  const idleLatencyMs = ref(0) // 延迟阶段测出的空闲延迟(latency 帧即有值,不必等 done)
  const jitterMs = ref(0)
  const result = shallowRef<SpeedtestOutcome | null>(null)
  const error = ref('')
  let controller: AbortController | null = null

  const start = async (nodeKey: string): Promise<SpeedtestOutcome | null> => {
    if (running.value) return null
    running.value = true
    error.value = ''
    liveMbps.value = 0
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
            liveMbps.value = 0 // 阶段切换时重置实时数字
          },
          onSample: (_p: SpeedtestPhase, mbps: number) => {
            liveMbps.value = mbps
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
    idleLatencyMs,
    jitterMs,
    result,
    error,
    start,
    cancel
  }
}
