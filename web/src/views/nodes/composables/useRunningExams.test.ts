import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useRunningExams } from './useRunningExams'
import * as jobsApi from '@/api/jobs'
import type { Job } from '@/api/jobs'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

vi.mock('@/api/jobs')

describe('useRunningExams', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  const mountComposable = () => {
    let result: ReturnType<typeof useRunningExams> | null = null
    const TestComponent = defineComponent({
      setup() {
        result = useRunningExams()
        return () => h('div')
      }
    })
    const wrapper = mount(TestComponent)
    return { wrapper, result: result! }
  }

  it('loads running exam jobs and extracts keys', async () => {
    const mockJobs: Job[] = [
      {
        id: 1,
        kind: 'exam',
        key: 'node1.example.com:443',
        status: 'running',
        created_at: '2026-07-20T10:00:00Z',
        updated_at: '2026-07-20T10:00:00Z'
      },
      {
        id: 2,
        kind: 'exam',
        key: 'node2.example.com:443',
        status: 'done',
        created_at: '2026-07-20T09:00:00Z',
        updated_at: '2026-07-20T09:30:00Z'
      },
      {
        id: 3,
        kind: 'batch_exam',
        key: 'batch-001',
        status: 'running',
        created_at: '2026-07-20T08:00:00Z',
        updated_at: '2026-07-20T10:00:00Z'
      },
      {
        id: 4,
        kind: 'exam',
        key: 'node3.example.com:443',
        status: 'running',
        created_at: '2026-07-20T10:05:00Z',
        updated_at: '2026-07-20T10:05:00Z'
      }
    ]

    vi.mocked(jobsApi.listJobs).mockResolvedValue(mockJobs)

    const { result } = mountComposable()
    await vi.runAllTimersAsync()

    // Should only include running exam jobs, not done or batch_exam
    expect(result.runningExamKeys.value.size).toBe(2)
    expect(result.runningExamKeys.value.has('node1.example.com:443')).toBe(true)
    expect(result.runningExamKeys.value.has('node3.example.com:443')).toBe(true)
    expect(result.runningExamKeys.value.has('node2.example.com:443')).toBe(false)
    expect(result.runningExamKeys.value.has('batch-001')).toBe(false)
  })

  it('polls every 10 seconds', async () => {
    vi.mocked(jobsApi.listJobs).mockResolvedValue([])

    mountComposable()

    // Initial load
    await vi.runAllTimersAsync()
    expect(jobsApi.listJobs).toHaveBeenCalledTimes(1)

    // Advance 10s
    await vi.advanceTimersByTimeAsync(10000)
    expect(jobsApi.listJobs).toHaveBeenCalledTimes(2)

    // Advance another 10s
    await vi.advanceTimersByTimeAsync(10000)
    expect(jobsApi.listJobs).toHaveBeenCalledTimes(3)
  })

  it('handles API errors gracefully', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(jobsApi.listJobs).mockRejectedValue(new Error('Network error'))

    const { result } = mountComposable()
    await vi.runAllTimersAsync()

    // Should not crash, set should be empty
    expect(result.runningExamKeys.value.size).toBe(0)
    expect(consoleErrorSpy).toHaveBeenCalledWith('Failed to load running exams:', expect.any(Error))
  })

  it('reload method triggers immediate update', async () => {
    const mockJobs: Job[] = [
      {
        id: 1,
        kind: 'exam',
        key: 'node1.example.com:443',
        status: 'running',
        created_at: '2026-07-20T10:00:00Z',
        updated_at: '2026-07-20T10:00:00Z'
      }
    ]

    vi.mocked(jobsApi.listJobs).mockResolvedValue(mockJobs)

    const { result } = mountComposable()
    await vi.runAllTimersAsync()

    expect(result.runningExamKeys.value.size).toBe(1)

    // Change mock data
    vi.mocked(jobsApi.listJobs).mockResolvedValue([])

    // Manual reload
    await result.reload()
    expect(result.runningExamKeys.value.size).toBe(0)
  })
})
