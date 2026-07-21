import { describe, it, expect } from 'vitest'
import { generateQRCode } from './useQRCode'

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
})
