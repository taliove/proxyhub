// 实测运行的状态机:封装 runner,对外暴露阶段、最终结果与取消。
// 纯状态管理,不碰 DOM;落库与历史刷新由调用侧(页面)在 onDone 后串接。
// nodeKey 由调用侧传入(空串 = 直连基线),runner 据此调后端代理测速 API。
import { ref, shallowRef } from 'vue'
import { runSpeedtest, type SpeedtestOutcome, type SpeedtestPhase } from '../runner'

export function useSpeedtestRun() {
  // phase: null = 空闲;result: 最近一次完成的产出
  const phase = ref<SpeedtestPhase | null>(null)
  const running = ref(false)
  const result = shallowRef<SpeedtestOutcome | null>(null)
  const error = ref('')
  let controller: AbortController | null = null

  const start = async (nodeKey: string): Promise<SpeedtestOutcome | null> => {
    if (running.value) return null
    running.value = true
    error.value = ''
    controller = new AbortController()
    try {
      const outcome = await runSpeedtest(
        nodeKey,
        {
          onPhase: (p: SpeedtestPhase) => {
            phase.value = p
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

  return { phase, running, result, error, start, cancel }
}
