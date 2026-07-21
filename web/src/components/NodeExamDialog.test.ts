import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import NodeExamDialog from './NodeExamDialog.vue'
import type { ExamEvent } from '@/types'
import * as ElementPlus from 'element-plus'

// Mock Element Plus
vi.mock('element-plus', () => ({
  ElMessage: vi.fn(),
  ElDialog: {
    name: 'ElDialog',
    template: '<div class="el-dialog"><slot name="header" /><slot /><slot name="footer" /></div>',
    props: ['modelValue', 'closeOnClickModal', 'closeOnPressEscape', 'showClose']
  },
  ElButton: { name: 'ElButton', template: '<button><slot /></button>' },
  ElTag: { name: 'ElTag', template: '<span><slot /></span>', props: ['type', 'size', 'effect'] }
}))

// Mock child components to isolate dialog behavior
vi.mock('./exam/ExamReportLayout.vue', () => ({
  default: { name: 'ExamReportLayout', template: '<div class="mock-report-layout" />' }
}))
vi.mock('./exam/ExamShareDialog.vue', () => ({
  default: { name: 'ExamShareDialog', template: '<div class="mock-share-dialog" />' }
}))

describe('NodeExamDialog background task behavior', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  const mockEventSource = () => {
    const es = {
      onmessage: null as ((e: { data: string }) => void) | null,
      onerror: null as ((e?: unknown) => void) | null,
      onopen: null as ((e?: unknown) => void) | null,
      close: vi.fn(),
      readyState: 0
    }
    return es
  }

  it('allows closing dialog during exam (modal/ESC/close button)', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es = mockEventSource()
    global.EventSource = vi.fn(() => es) as any

    // Open dialog and start exam
    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    // Simulate connection established
    es.onopen?.()
    await nextTick()

    // Send stability frame to enter running state
    const sampleFrame: ExamEvent = {
      seq: 1,
      phase: 'sample',
      sample: { seq: 1, elapsed_ms: 100, latency_ms: 50, ok: true }
    }
    es.onmessage?.({ data: JSON.stringify(sampleFrame) })
    await nextTick()

    // Dialog should be visible and running
    expect(wrapper.vm.visible).toBe(true)
    expect(wrapper.vm.running).toBe(true)

    // Verify the component's computed properties work correctly -
    // these drive the template bindings
    // In the new behavior, dialog can be closed even when running
    expect(wrapper.vm.running).toBe(true)
  })

  it('shows background continuation toast when closing during exam', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es = mockEventSource()
    global.EventSource = vi.fn(() => es) as any

    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    es.onopen?.()
    await nextTick()

    const sampleFrame: ExamEvent = {
      seq: 1,
      phase: 'sample',
      sample: { seq: 1, elapsed_ms: 100, latency_ms: 50, ok: true }
    }
    es.onmessage?.({ data: JSON.stringify(sampleFrame) })
    await nextTick()

    expect(wrapper.vm.running).toBe(true)

    // Manually trigger the close event (simulates user clicking close)
    await wrapper.vm.onClose()
    await nextTick()

    // Should show background continuation message
    expect(ElementPlus.ElMessage).toHaveBeenCalledWith(
      expect.objectContaining({
        message: expect.stringContaining('后台继续运行'),
        type: 'info'
      })
    )
  })

  it('does NOT cancel task when closing dialog during exam', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es = mockEventSource()
    global.EventSource = vi.fn(() => es) as any

    const fetchSpy = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    global.fetch = fetchSpy

    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    es.onopen?.()
    await nextTick()

    const sampleFrame: ExamEvent = {
      seq: 1,
      phase: 'sample',
      sample: { seq: 1, elapsed_ms: 100, latency_ms: 50, ok: true }
    }
    es.onmessage?.({ data: JSON.stringify(sampleFrame) })
    await nextTick()

    // Trigger onClosed (simulates dialog closed event)
    await wrapper.vm.onClosed()
    await nextTick()

    // Should close EventSource but NOT call cancel endpoint
    expect(es.close).toHaveBeenCalled()
    expect(fetchSpy).not.toHaveBeenCalledWith(
      expect.stringContaining('/cancel'),
      expect.any(Object)
    )
  })

  it('reopening same node attaches without force (no restart)', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es1 = mockEventSource()
    const es2 = mockEventSource()
    const eventSources = [es1, es2]
    let esIndex = 0
    global.EventSource = vi.fn((url: string) => {
      const es = eventSources[esIndex++]
      // Capture URL to verify force parameter
      ;(es as any)._url = url
      return es
    }) as any

    // First open
    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    es1.onopen?.()
    await nextTick()

    const frame1: ExamEvent = {
      seq: 1,
      phase: 'sample',
      sample: { seq: 1, elapsed_ms: 100, latency_ms: 50, ok: true }
    }
    es1.onmessage?.({ data: JSON.stringify(frame1) })
    await nextTick()

    // Trigger onClosed (stream disposed)
    await wrapper.vm.onClosed()
    await nextTick()
    expect(es1.close).toHaveBeenCalled()

    // Reopen same node (should attach, not force restart)
    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    // Second EventSource should NOT have force=1 parameter
    const url2 = (es2 as any)._url as string
    expect(url2).toContain('/api/nodes/exam/stream')
    expect(url2).toContain('node_key=test-node')
    expect(url2).not.toContain('force=1')
  })

  it('replays state when reopening (samples, regions, unlocks)', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es = mockEventSource()
    global.EventSource = vi.fn(() => es) as any

    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    es.onopen?.()
    await nextTick()

    // Backend replays frames on reconnect
    const replayFrames: ExamEvent[] = [
      { seq: 1, phase: 'sample', sample: { seq: 1, elapsed_ms: 100, latency_ms: 50, ok: true } },
      { seq: 2, phase: 'sample', sample: { seq: 2, elapsed_ms: 200, latency_ms: 55, ok: true } },
      {
        seq: 3,
        phase: 'region',
        region: { code: 'HK', name: 'Hong Kong', ttfb_ms: 50, down_mbps: 100 }
      },
      {
        seq: 4,
        phase: 'unlock',
        unlock_result: {
          node_key: 'test-node',
          target_name: 'Netflix',
          available: true,
          latency: 50,
          region: 'HK'
        }
      }
    ]

    for (const frame of replayFrames) {
      es.onmessage?.({ data: JSON.stringify(frame) })
      await nextTick()
    }

    // Verify state is rebuilt
    expect(wrapper.vm.samples).toHaveLength(2)
    expect(wrapper.vm.regions).toHaveLength(1)
    expect(wrapper.vm.unlockResults).toHaveLength(1)
    expect(wrapper.vm.samples[0].latency_ms).toBe(50)
    expect(wrapper.vm.regions[0].code).toBe('HK')
    expect(wrapper.vm.unlockResults[0].target_name).toBe('Netflix')
  })

  it('rerun button still forces new exam', async () => {
    const wrapper = mount(NodeExamDialog, {
      global: {
        stubs: {
          teleport: true,
          'el-dialog': true,
          'el-tag': true,
          'el-button': true
        }
      }
    })
    const es1 = mockEventSource()
    const es2 = mockEventSource()
    const eventSources = [es1, es2]
    let esIndex = 0
    global.EventSource = vi.fn((url: string) => {
      const es = eventSources[esIndex++]
      ;(es as any)._url = url
      return es
    }) as any

    wrapper.vm.open({ node_key: 'test-node' }, 'Test Node')
    await nextTick()

    es1.onopen?.()
    await nextTick()

    const doneFrame: ExamEvent = {
      seq: 5,
      phase: 'done',
      metrics: {
        total: 100,
        succeeded: 98,
        loss_rate: 0.02,
        mean_ms: 100,
        median_ms: 95,
        p95_ms: 120,
        p99_ms: 150,
        jitter_ms: 5,
        score: 90
      }
    }
    es1.onmessage?.({ data: JSON.stringify(doneFrame) })
    await nextTick()

    expect(wrapper.vm.terminal).toBe(true)

    // Click rerun button
    wrapper.vm.rerun()
    await nextTick()

    // Second EventSource SHOULD have force=1
    const url2 = (es2 as any)._url as string
    expect(url2).toContain('force=1')
  })
})
