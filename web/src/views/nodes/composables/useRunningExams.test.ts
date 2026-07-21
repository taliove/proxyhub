import { describe, it, expect, vi, afterEach } from 'vitest'
import { useRunningExams } from './useRunningExams'
import * as jobsApi from '@/api/jobs'
import type { Job } from '@/api/jobs'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

vi.mock('@/api/jobs')

// 测试用真实定时器 + 注入短轮询间隔(假定时器与 async load 叠加会自旋)。
const POLL_MS = 15

const flush = (ms = 60) => new Promise((r) => setTimeout(r, ms))

const job = (id: number, kind: string, key: string, status: string): Job => ({
  id,
  kind,
  key,
  status,
  created_at: '2026-07-20T10:00:00Z',
  updated_at: '2026-07-20T10:00:00Z'
})

describe('useRunningExams', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  const mountComposable = () => {
    let result: ReturnType<typeof useRunningExams> | null = null
    const TestComponent = defineComponent({
      setup() {
        result = useRunningExams(POLL_MS)
        return () => h('div')
      }
    })
    const wrapper = mount(TestComponent)
    return { wrapper, result: result! }
  }

  it('loads running exam jobs and extracts keys', async () => {
    vi.mocked(jobsApi.listJobs).mockResolvedValue([
      job(1, 'exam', 'node1.example.com:443', 'running'),
      job(2, 'exam', 'node2.example.com:443', 'done'),
      job(3, 'batch_exam', 'batch-001', 'running'),
      job(4, 'exam', 'node3.example.com:443', 'running')
    ])

    const { wrapper, result } = mountComposable()
    await flush()

    expect(result.runningExamKeys.value.size).toBe(2)
    expect(result.runningExamKeys.value.has('node1.example.com:443')).toBe(true)
    expect(result.runningExamKeys.value.has('node3.example.com:443')).toBe(true)
    expect(result.runningExamKeys.value.has('node2.example.com:443')).toBe(false)
    expect(result.runningExamKeys.value.has('batch-001')).toBe(false)
    wrapper.unmount()
  })

  it('polls on the injected interval', async () => {
    vi.mocked(jobsApi.listJobs).mockResolvedValue([])

    const { wrapper } = mountComposable()
    await flush()

    const calls = vi.mocked(jobsApi.listJobs).mock.calls.length
    expect(calls).toBeGreaterThanOrEqual(2) // 立即一次 + 至少一次轮询
    wrapper.unmount()
  })

  it('handles API errors gracefully', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(jobsApi.listJobs).mockRejectedValue(new Error('Network error'))

    const { wrapper, result } = mountComposable()
    await flush(40)

    expect(result.runningExamKeys.value.size).toBe(0)
    expect(consoleErrorSpy).toHaveBeenCalledWith('Failed to load running exams:', expect.any(Error))
    wrapper.unmount()
  })

  it('reload method triggers immediate update', async () => {
    vi.mocked(jobsApi.listJobs).mockResolvedValue([
      job(1, 'exam', 'node1.example.com:443', 'running')
    ])

    const { wrapper, result } = mountComposable()
    await flush()
    expect(result.runningExamKeys.value.size).toBe(1)

    vi.mocked(jobsApi.listJobs).mockResolvedValue([])
    await result.reload()
    expect(result.runningExamKeys.value.size).toBe(0)
    wrapper.unmount()
  })
})
