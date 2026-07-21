import QRCode from 'qrcode'

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
