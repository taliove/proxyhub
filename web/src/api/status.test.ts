// getStatus 测试:请求 /status 并透传版本自报字段
import { describe, it, expect, vi, beforeEach } from 'vitest'
import client from '@/api/client'
import { getStatus } from '@/api/status'

vi.mock('@/api/client')

describe('getStatus', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('请求 /status 并返回版本字段', async () => {
    vi.mocked(client.get).mockResolvedValue({
      initialized: true,
      version: '1.2.3',
      build_time: '2026-07-28_00:00:00'
    })
    const s = await getStatus()
    expect(client.get).toHaveBeenCalledWith('/status')
    expect(s.version).toBe('1.2.3')
    expect(s.build_time).toBe('2026-07-28_00:00:00')
  })
})
