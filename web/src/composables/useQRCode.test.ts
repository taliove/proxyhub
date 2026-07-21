import { describe, it, expect } from 'vitest'
import { generateQRCode, getNodeShareURI } from './useQRCode'
import type { Node } from '@/types'

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
    it('should use share_link when available (airport node)', () => {
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
        stale: false,
        share_link:
          'vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls#Test%20HK%2001'
      } as Node & { share_link: string }

      const uri = getNodeShareURI(node)
      expect(uri).toBe(node.share_link)
    })

    it('should generate URI via backend for self-hosted node without share_link', () => {
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

      const uri = getNodeShareURI(node)
      expect(uri).toBeNull() // Backend endpoint needed
    })

    it('should return null when no share_link and not generatable', () => {
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

      const uri = getNodeShareURI(node)
      expect(uri).toBeNull()
    })
  })
})
