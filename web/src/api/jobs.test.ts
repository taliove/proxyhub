// listJobs 过滤参数测试:缺省向后兼容,kind/status 拼 query
import { describe, it, expect, vi, beforeEach } from 'vitest'
import client from '@/api/client'
import { listJobs } from '@/api/jobs'

vi.mock('@/api/client')

describe('listJobs', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('不传过滤参数时请求 /jobs(向后兼容)', async () => {
    vi.mocked(client.get).mockResolvedValue([])
    await listJobs()
    expect(client.get).toHaveBeenCalledWith('/jobs')
  })

  it('传 kind/status 时拼为 query 参数', async () => {
    vi.mocked(client.get).mockResolvedValue([])
    await listJobs({ kind: 'refresh', status: 'failed' })
    expect(client.get).toHaveBeenCalledWith('/jobs?kind=refresh&status=failed')
  })

  it('只传 status 时只带 status', async () => {
    vi.mocked(client.get).mockResolvedValue([])
    await listJobs({ status: 'interrupted' })
    expect(client.get).toHaveBeenCalledWith('/jobs?status=interrupted')
  })
})
