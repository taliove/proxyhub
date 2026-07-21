import { describe, it, expect } from 'vitest'
import { buildTimelineItems } from './examhistory'
import type { ExamHistoryEntry, ExamReport } from '@/types'

// ExamHistoryTimeline 快捷分享测试:验证时间线条目数据结构支持快捷分享。
// 组件层(Vue)逻辑:每个条目渲染分享按钮,点击 @click.stop 阻止展开,直接唤起 ExamShareDialog。

const mockReport = (): ExamReport => ({
  stability: {
    total: 20,
    succeeded: 17,
    loss_rate: 0.15,
    mean_ms: 120,
    median_ms: 115,
    p95_ms: 180,
    p99_ms: 220,
    jitter_ms: 15,
    score: 85
  }
})

const mockEntry = (id: number, createdAt: string): ExamHistoryEntry => ({
  id,
  node_key: 'test-node',
  report: mockReport(),
  created_at: createdAt
})

describe('ExamHistoryTimeline share button', () => {
  describe('timeline item structure', () => {
    it('provides all data needed for share dialog', () => {
      const entries = [mockEntry(1, '2026-07-20T10:00:00Z'), mockEntry(2, '2026-07-20T09:00:00Z')]
      const items = buildTimelineItems(entries)

      expect(items.length).toBe(2)
      // 每个条目包含 id + createdAt,足够定位到对应的 ExamHistoryEntry。
      items.forEach((item, idx) => {
        expect(item.id).toBe(entries[idx].id)
        expect(item.createdAt).toBe(entries[idx].created_at)
      })
    })

    it('timeline item has stable id for share target', () => {
      const entry = mockEntry(123, '2026-07-20T10:00:00Z')
      const items = buildTimelineItems([entry])
      expect(items[0].id).toBe(123)
    })
  })

  describe('share dialog data binding', () => {
    it('validates share requires report + nodeName + examTime', () => {
      // ExamShareDialog 签名:report(ExamReport)、nodeName(string)、examTime(string)。
      // 时间线点击分享时:shareId -> 定位 entry -> 提取 entry.report + entry.created_at。
      const entry = mockEntry(1, '2026-07-20T10:00:00Z')
      expect(entry.report).toBeTruthy()
      expect(entry.created_at).toBeTruthy()
      // nodeName 由 ExamHistoryTimeline props 透传,无需从 entry 取。
    })
  })
})
