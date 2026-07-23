// 实测运行的状态机:封装 runner,对外暴露阶段、实时速率、最终结果与取消。
// 纯状态管理,不碰 DOM;落库与历史刷新由调用侧(页面)在 onDone 后串接。
import { ref, shallowRef } from 'vue'
import { runSpeedtest, type SpeedtestOutcome, type SpeedtestPhase } from '../runner'

export function useSpeedtestRun() {
  // phase: null = 空闲;result: 最近一次完成的产出
  const phase = ref<SpeedtestPhase | null>(null)
  const running = ref(false)
  const liveMbps = ref(0) // 当前阶段实时速率(下行/上行大数字实时刷新)
  const result = shallowRef<SpeedtestOutcome | null>(null)
  const error = ref('')
  let controller: AbortController | null = null

  const start = async (): Promise<SpeedtestOutcome | null> => {
    if (running.value) return null
    running.value = true
    error.value = ''
    liveMbps.value = 0
    controller = new AbortController()
    try {
      const outcome = await runSpeedtest(
        {
          onPhase: (p) => {
            phase.value = p
            liveMbps.value = 0
          },
          onSample: (_p, mbps) => {
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

  return { phase, running, liveMbps, result, error, start, cancel }
}
