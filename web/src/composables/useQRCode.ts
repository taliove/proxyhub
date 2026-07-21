import QRCode from 'qrcode'
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
 * Get node share URI for QR code generation
 * For airport nodes: use share_link from subscription if available
 * For self-hosted nodes: need backend API to generate
 * @param node Node object
 * @returns Share URI string or null if not available
 */
export function getNodeShareURI(node: Node): string | null {
  // Extended Node type may have share_link from subscription parse
  const nodeWithLink = node as Node & { share_link?: string }

  if (nodeWithLink.share_link) {
    return nodeWithLink.share_link
  }

  // For nodes without share_link, we need backend generation
  // This will be handled by calling /api/nodes/{node_key}/share-uri
  return null
}
