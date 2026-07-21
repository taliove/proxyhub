import { describe, it, expect } from 'vitest'
import type { Airport } from '@/types'
import { getAirportQRContent } from './airport-utils'

describe('Airports QR Code', () => {
  describe('getAirportQRContent', () => {
    it('should extract subscription URL from airport', () => {
      const airport: Airport = {
        id: 1,
        name: 'Test Airport',
        url: 'https://example.com/subscribe/token123',
        abbr: 'TA',
        enabled: true,
        created_at: '2026-01-01T00:00:00Z'
      }

      const content = getAirportQRContent(airport)
      expect(content).toBe('https://example.com/subscribe/token123')
    })

    it('should handle airport with empty abbr', () => {
      const airport: Airport = {
        id: 2,
        name: 'Another Airport',
        url: 'https://another.example.com/sub/abc',
        abbr: '',
        enabled: false,
        created_at: '2026-01-01T00:00:00Z'
      }

      const content = getAirportQRContent(airport)
      expect(content).toBe('https://another.example.com/sub/abc')
    })
  })
})
