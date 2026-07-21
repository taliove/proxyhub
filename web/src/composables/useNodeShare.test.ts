import { describe, it, expect, vi, beforeEach } from 'vitest'
import { canGenerateShareLink, copyNodeLink, getNodeShareLink } from './useNodeShare'
import type { Node } from '@/types'
import client from '@/api/client'
import { ElMessage } from 'element-plus'

vi.mock('@/api/client')
vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn()
  }
}))

// Mock clipboard API
Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn()
  }
})

describe('useNodeShare', () => {
  const createNode = (overrides: Partial<Node> = {}): Node => ({
    name: 'Test Node',
    display_name: 'Test Display',
    type: 'vmess',
    server: 'example.com',
    port: 443,
    network: 'tcp',
    tls: true,
    region: 'HK',
    source: 'Test Airport',
    latency: 50,
    available: true,
    node_key: 'example.com:443',
    blocked: false,
    stale: false,
    ...overrides
  })

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('canGenerateShareLink', () => {
    it('should return true for vmess protocol', () => {
      const node = createNode({ type: 'vmess' })
      expect(canGenerateShareLink(node)).toBe(true)
    })

    it('should return true for vless protocol', () => {
      const node = createNode({ type: 'vless' })
      expect(canGenerateShareLink(node)).toBe(true)
    })

    it('should return true for trojan protocol', () => {
      const node = createNode({ type: 'trojan' })
      expect(canGenerateShareLink(node)).toBe(true)
    })

    it('should return true for ss protocol', () => {
      const node = createNode({ type: 'ss' })
      expect(canGenerateShareLink(node)).toBe(true)
    })

    it('should return true for anytls protocol', () => {
      const node = createNode({ type: 'anytls' })
      expect(canGenerateShareLink(node)).toBe(true)
    })

    it('should return false for unsupported protocol', () => {
      const node = createNode({ type: 'http' })
      expect(canGenerateShareLink(node)).toBe(false)
    })

    it('should return false for unknown protocol', () => {
      const node = createNode({ type: 'unknown' })
      expect(canGenerateShareLink(node)).toBe(false)
    })
  })

  describe('getNodeShareLink', () => {
    it('should fetch share link from backend', async () => {
      const node = createNode({ type: 'vmess' })
      const mockUri = 'vmess://test-encoded-uri'
      vi.mocked(client.get).mockResolvedValueOnce({ uri: mockUri })

      const result = await getNodeShareLink(node)

      expect(result).toBe(mockUri)
      expect(client.get).toHaveBeenCalledWith('/nodes/example.com%3A443/share-uri')
    })

    it('should URL encode node_key with special characters', async () => {
      const node = createNode({ node_key: '[::1]:8080' })
      const mockUri = 'vmess://test-encoded-uri'
      vi.mocked(client.get).mockResolvedValueOnce({ uri: mockUri })

      await getNodeShareLink(node)

      expect(client.get).toHaveBeenCalledWith(
        `/nodes/${encodeURIComponent('[::1]:8080')}/share-uri`
      )
    })

    it('should throw error when backend fails', async () => {
      const node = createNode({ type: 'vmess' })
      const errorMessage = 'Node not found'
      vi.mocked(client.get).mockRejectedValueOnce(new Error(errorMessage))

      await expect(getNodeShareLink(node)).rejects.toThrow()
    })

    it('should throw error for unsupported protocol', async () => {
      const node = createNode({ type: 'http' })

      await expect(getNodeShareLink(node)).rejects.toThrow(
        'Unsupported protocol: http. Only vmess, vless, trojan, ss, anytls are supported.'
      )
    })
  })

  describe('copyNodeLink', () => {
    it('should copy link and show success message', async () => {
      const node = createNode({ type: 'vmess' })
      const mockUri = 'vmess://test-encoded-uri'
      vi.mocked(client.get).mockResolvedValueOnce({ uri: mockUri })
      vi.mocked(navigator.clipboard.writeText).mockResolvedValueOnce()

      await copyNodeLink(node)

      expect(client.get).toHaveBeenCalledWith('/nodes/example.com%3A443/share-uri')
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(mockUri)
      expect(ElMessage.success).toHaveBeenCalledWith('节点链接已复制到剪贴板')
    })

    it('should show error message when API fails', async () => {
      const node = createNode({ type: 'vmess' })
      const error = new Error('Network error')
      vi.mocked(client.get).mockRejectedValueOnce(error)

      await copyNodeLink(node)

      expect(ElMessage.error).toHaveBeenCalledWith('复制失败: Network error')
    })

    it('should show error message when clipboard write fails', async () => {
      const node = createNode({ type: 'vmess' })
      const mockUri = 'vmess://test-encoded-uri'
      vi.mocked(client.get).mockResolvedValueOnce({ uri: mockUri })
      vi.mocked(navigator.clipboard.writeText).mockRejectedValueOnce(new Error('Clipboard denied'))

      await copyNodeLink(node)

      expect(ElMessage.error).toHaveBeenCalledWith('复制失败: Clipboard denied')
    })

    it('should show error for unsupported protocol', async () => {
      const node = createNode({ type: 'http' })

      await copyNodeLink(node)

      expect(ElMessage.error).toHaveBeenCalledWith(
        '不支持的协议: http. 仅支持 vmess, vless, trojan, ss, anytls.'
      )
    })
  })
})
