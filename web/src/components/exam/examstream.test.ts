import { describe, it, expect, vi } from 'vitest'
import {
  MAX_RECONNECT_ATTEMPTS,
  reconnectDelayMs,
  shouldReconnect,
  isNewSeq,
  ExamStream
} from './examstream'
import type { EventSourceLike, ExamStreamStatus } from './examstream'
import type { ExamEvent } from '@/types'

describe('reconnectDelayMs / shouldReconnect', () => {
  it('grows exponentially and caps at 8000ms', () => {
    expect(reconnectDelayMs(0)).toBe(500)
    expect(reconnectDelayMs(1)).toBe(1000)
    expect(reconnectDelayMs(2)).toBe(2000)
    expect(reconnectDelayMs(3)).toBe(4000)
    expect(reconnectDelayMs(4)).toBe(8000)
    expect(reconnectDelayMs(9)).toBe(8000)
  })

  it('allows at most MAX_RECONNECT_ATTEMPTS attempts', () => {
    expect(MAX_RECONNECT_ATTEMPTS).toBe(5)
    expect(shouldReconnect(0)).toBe(true)
    expect(shouldReconnect(4)).toBe(true)
    expect(shouldReconnect(5)).toBe(false)
  })
})

describe('isNewSeq', () => {
  it('accepts strictly increasing seq and rejects replays', () => {
    expect(isNewSeq(0, 1)).toBe(true)
    expect(isNewSeq(5, 6)).toBe(true)
    expect(isNewSeq(5, 5)).toBe(false)
    expect(isNewSeq(5, 3)).toBe(false)
  })
  it('accepts frames without a seq (lenient)', () => {
    expect(isNewSeq(5, undefined)).toBe(true)
  })
})

// --- test doubles ------------------------------------------------------------

class FakeES implements EventSourceLike {
  onmessage: ((e: { data: string }) => void) | null = null
  onerror: ((e?: unknown) => void) | null = null
  onopen: ((e?: unknown) => void) | null = null
  closed = false
  constructor(public url: string) {}
  close() {
    this.closed = true
  }
  open() {
    this.onopen?.()
  }
  emit(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) })
  }
  fail() {
    this.onerror?.()
  }
}

interface Harness {
  stream: ExamStream
  sources: FakeES[]
  frames: ExamEvent[]
  statuses: ExamStreamStatus[]
  fetchMock: ReturnType<typeof vi.fn>
  runTimer: () => void
  pendingDelays: number[]
}

function makeHarness(): Harness {
  const sources: FakeES[] = []
  const frames: ExamEvent[] = []
  const statuses: ExamStreamStatus[] = []
  const timers: Array<{ cb: () => void; delay: number }> = []
  const pendingDelays: number[] = []
  const fetchMock = vi.fn(() => Promise.resolve({ ok: true } as Response))

  const stream = new ExamStream(
    {
      createEventSource: (url: string) => {
        const es = new FakeES(url)
        sources.push(es)
        return es
      },
      fetch: fetchMock as unknown as typeof fetch,
      setTimer: (cb: () => void, delay: number) => {
        timers.push({ cb, delay })
        pendingDelays.push(delay)
        return timers.length - 1
      },
      clearTimer: () => {}
    },
    {
      onFrame: (f: ExamEvent) => frames.push(f),
      onStatus: (s: ExamStreamStatus) => statuses.push(s)
    }
  )

  const runTimer = () => {
    const t = timers.shift()
    if (t) t.cb()
  }

  return { stream, sources, frames, statuses, fetchMock, runTimer, pendingDelays }
}

const sample = (seq: number): ExamEvent => ({
  phase: 'sample',
  seq,
  sample: { seq, elapsed_ms: 0, latency_ms: 100, ok: true }
})

describe('ExamStream lifecycle', () => {
  it('emits connecting then live and forwards frames', () => {
    const h = makeHarness()
    h.stream.start('/stream')
    expect(h.statuses).toEqual(['connecting'])
    h.sources[0].open()
    expect(h.statuses).toEqual(['connecting', 'live'])
    h.sources[0].emit(sample(1))
    h.sources[0].emit(sample(2))
    expect(h.frames.map((f) => f.seq)).toEqual([1, 2])
  })

  it('marks done on a done frame and stops reconnecting on later errors', () => {
    const h = makeHarness()
    h.stream.start('/stream')
    h.sources[0].open()
    h.sources[0].emit({ phase: 'done' })
    expect(h.statuses.at(-1)).toBe('done')
    expect(h.sources[0].closed).toBe(true)
    h.sources[0].fail()
    // no reconnect scheduled after terminal
    expect(h.sources).toHaveLength(1)
  })
})

describe('ExamStream reconnect + dedup', () => {
  it('reconnects with backoff, replays are deduped by seq, resumes seamlessly', () => {
    const h = makeHarness()
    h.stream.start('/stream')
    h.sources[0].open()
    h.sources[0].emit(sample(1))
    h.sources[0].emit(sample(2))

    // connection drops
    h.sources[0].fail()
    expect(h.statuses.at(-1)).toBe('reconnecting')
    expect(h.sources[0].closed).toBe(true)
    expect(h.pendingDelays.at(-1)).toBe(500)

    // backoff elapses -> new EventSource opened
    h.runTimer()
    expect(h.sources).toHaveLength(2)
    h.sources[1].open()

    // server replays 1,2 (deduped) then live 3
    h.sources[1].emit(sample(1))
    h.sources[1].emit(sample(2))
    h.sources[1].emit(sample(3))
    expect(h.frames.map((f) => f.seq)).toEqual([1, 2, 3])
  })

  it('gives up after MAX_RECONNECT_ATTEMPTS failures with error status', () => {
    const h = makeHarness()
    h.stream.start('/stream')
    h.sources[0].open()

    // 5 failures each followed by the backoff timer firing (reopen)
    for (let i = 0; i < MAX_RECONNECT_ATTEMPTS; i++) {
      h.sources.at(-1)!.fail()
      expect(h.statuses.at(-1)).toBe('reconnecting')
      h.runTimer()
    }
    // 6th failure exhausts the budget
    h.sources.at(-1)!.fail()
    expect(h.statuses.at(-1)).toBe('error')
  })

  it('resets the reconnect budget after a successful reopen', () => {
    const h = makeHarness()
    h.stream.start('/stream')
    h.sources[0].open()
    h.sources[0].fail() // attempt 1 -> 500ms
    h.runTimer()
    h.sources[1].open() // success resets budget
    h.sources[1].fail() // attempt should start again at 500ms
    expect(h.pendingDelays.at(-1)).toBe(500)
  })
})

describe('ExamStream cancel', () => {
  it('POSTs the cancel url and surfaces the cancelled frame as terminal', async () => {
    const h = makeHarness()
    h.stream.start('/stream')
    h.sources[0].open()
    h.sources[0].emit(sample(1))

    await h.stream.cancel('/cancel')
    expect(h.fetchMock).toHaveBeenCalledWith('/cancel', expect.objectContaining({ method: 'POST' }))

    // partial result (frame 1) already delivered and retained by caller
    expect(h.frames.map((f) => f.seq)).toEqual([1])

    // server pushes cancelled
    h.sources[0].emit({ phase: 'cancelled' })
    expect(h.statuses.at(-1)).toBe('cancelled')
    expect(h.sources[0].closed).toBe(true)
  })
})
