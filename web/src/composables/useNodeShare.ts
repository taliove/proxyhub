import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'

const SUPPORTED_PROTOCOLS = ['vmess', 'vless', 'trojan', 'ss', 'anytls'] as const

/**
 * Check if a node can generate share link
 * @param node Node object
 * @returns true if protocol is supported
 */
export function canGenerateShareLink(node: Node): boolean {
  return SUPPORTED_PROTOCOLS.includes(node.type as (typeof SUPPORTED_PROTOCOLS)[number])
}

/**
 * Get node share link from backend API
 * @param node Node object
 * @returns Share URI string
 * @throws Error if protocol unsupported or API fails
 */
export async function getNodeShareLink(node: Node): Promise<string> {
  if (!canGenerateShareLink(node)) {
    throw new Error(
      `Unsupported protocol: ${node.type}. Only ${SUPPORTED_PROTOCOLS.join(', ')} are supported.`
    )
  }

  const encodedKey = encodeURIComponent(node.node_key)
  const response: { uri: string } = await client.get(`/nodes/${encodedKey}/share-uri`)
  return response.uri
}

/**
 * Copy node share link to clipboard with user feedback
 * @param node Node object
 */
export async function copyNodeLink(node: Node): Promise<void> {
  try {
    const uri = await getNodeShareLink(node)
    await navigator.clipboard.writeText(uri)
    ElMessage.success('节点链接已复制到剪贴板')
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    if (message.includes('Unsupported protocol')) {
      ElMessage.error(`不支持的协议: ${node.type}. 仅支持 ${SUPPORTED_PROTOCOLS.join(', ')}.`)
    } else {
      ElMessage.error(`复制失败: ${message}`)
    }
  }
}
