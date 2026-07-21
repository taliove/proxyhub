import { describe, it, expect, vi } from 'vitest'
import { generateQRCode, getNodeShareURI } from './useQRCode'
import { getNodeShareLink } from './useNodeShare'
import type { Node } from '@/types'

vi.mock('./useNodeShare', () => ({
  getNodeShareLink: vi.fn()
}))

describe('useQRCode', () => {
  describe('generateQRCode', () => {
    it('should generate data URL from text', async () => {
      const result = await generateQRCode('https://example.com/sub/test')
      expect(result).toMatch(/^data:image\/png;base64,/)
    })

    it('should throw error for empty text', async () => {
      await expect(generateQRCode('')).rejects.toThrow()
    })

    it('should handle long URLs', async () => {
      const longUrl = 'https://example.com/sub/' + 'x'.repeat(500)
      const result = await generateQRCode(longUrl)
      expect(result).toMatch(/^data:image\/png;base64,/)
    })
  })

  describe('getNodeShareURI', () => {
    it('should call backend API via getNodeShareLink', async () => {
      const node = {
        name: 'Test HK 01',
        display_name: '🇭🇰 香港 机场-01',
        type: 'vless',
        server: 'example.com',
        port: 443,
        network: 'tcp',
        tls: true,
        region: '香港',
        source: 'Airport A',
        latency: 50,
        available: true,
        node_key: 'example.com:443',
        blocked: false,
        stale: false
      } as Node

      const mockUri =
        'vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls#Test%20HK%2001'
      vi.mocked(getNodeShareLink).mockResolvedValueOnce(mockUri)

      const uri = await getNodeShareURI(node)
      expect(uri).toBe(mockUri)
      expect(getNodeShareLink).toHaveBeenCalledWith(node)
    })

    it('should work for self-hosted nodes', async () => {
      const node = {
        name: 'Self Node',
        display_name: 'Self Node',
        type: 'vless',
        server: 'example.com',
        port: 443,
        network: 'tcp',
        tls: true,
        region: '自建',
        source: 'self-hosted',
        latency: 30,
        available: true,
        node_key: 'example.com:443',
        blocked: false,
        stale: false
      } as Node

      const mockUri = 'vless://self-hosted-uri'
      vi.mocked(getNodeShareLink).mockResolvedValueOnce(mockUri)

      const uri = await getNodeShareURI(node)
      expect(uri).toBe(mockUri)
    })

    it('should throw error for unsupported protocol', async () => {
      const node = {
        name: 'Unknown Node',
        display_name: 'Unknown',
        type: 'unknown',
        server: 'example.com',
        port: 443,
        network: 'tcp',
        tls: false,
        region: 'Unknown',
        source: 'Unknown',
        latency: 0,
        available: false,
        node_key: 'example.com:443',
        blocked: false,
        stale: false
      } as Node

      const error = new Error('Unsupported protocol')
      vi.mocked(getNodeShareLink).mockRejectedValueOnce(error)

      await expect(getNodeShareURI(node)).rejects.toThrow('Unsupported protocol')
    })
  })
})
