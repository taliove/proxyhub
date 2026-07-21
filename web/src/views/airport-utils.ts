import type { Airport } from '@/types'

/**
 * Extract airport subscription URL for QR code display
 * @param airport Airport object containing subscription URL
 * @returns Airport subscription URL
 */
export function getAirportQRContent(airport: Airport): string {
  return airport.url
}
