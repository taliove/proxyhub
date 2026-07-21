import QRCode from 'qrcode'
import { getNodeShareLink } from './useNodeShare'
import type { Node } from '@/types'

/**
 * Generate QR code data URL from text content
 * @param text Content to encode (subscription URL or node share URI)
 * @returns Data URL (data:image/png;base64,...)
 * @throws Error if text is empty or generation fails
 */
export async function generateQRCode(text: string): Promise<string> {
  if (!text || text.trim() === '') {
    throw new Error('QR code content cannot be empty')
  }

  try {
    return await QRCode.toDataURL(text, {
      width: 300,
      margin: 2,
      errorCorrectionLevel: 'M'
    })
  } catch (err) {
    throw new Error(`Failed to generate QR code: ${err}`)
  }
}

/**
 * Get node share URI for QR code generation (legacy wrapper)
 * Now calls backend API for all nodes
 * @param node Node object
 * @returns Share URI string
 * @throws Error if protocol unsupported or backend fails
 */
export async function getNodeShareURI(node: Node): Promise<string> {
  return await getNodeShareLink(node)
}
