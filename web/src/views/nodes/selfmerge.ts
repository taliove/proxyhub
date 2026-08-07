import type { Node, SelfNode } from '@/types'
import { SELF_HOSTED } from './utils'

// 统一节点表把两处数据源合成一张表:
//   1. 节点池 /nodes——含机场节点与"已启用"的自建节点(后端 handleListNodes 经
//      mergeSelfHosted 以库为准 serve-time 合并,不依赖聚合刷新是否成功);
//   2. /self-nodes——自建节点全量(含已禁用),用于:
//      a) 给池中自建行补上 self_node_id / enabled(池视图不含这些字段),使内联编辑/启停/删除可用;
//      b) 把"已禁用"自建节点(不在池中)补进表格,否则禁用后将从界面消失、无法再管理(回归)。
// 全部为纯函数,返回新对象/新数组,不改入参。

// UnifiedNode 在只读节点视图上叠加自建管理所需字段。
export interface UnifiedNode extends Node {
  self_node_id?: number // 自建节点主键（机场节点为 undefined）
  enabled?: boolean // 自建节点启停态（机场节点为 undefined）
}

// 自建节点身份:服务器 + 端口 + 协议。用于把池中自建行反查回 SelfNode 记录。
export const selfIdentity = (server: string, port: number, protocol: string): string =>
  `${server}\0${port}\0${protocol}`

// selfNodeToRow 把禁用自建节点适配成表格行(最小展示 + 携带 id/enabled 供内联操作)。
export const selfNodeToRow = (sn: SelfNode): UnifiedNode => ({
  name: sn.name,
  display_name: '',
  type: sn.protocol,
  server: sn.server,
  port: sn.port,
  network: sn.network,
  tls: sn.tls,
  region: '',
  source: SELF_HOSTED,
  latency: 0,
  available: false,
  node_key: `self-node:${sn.id}`, // 禁用节点不在池中，用稳定合成键避免与池键冲突
  blocked: false,
  stale: false,
  availability_source: 'never', // 禁用节点不在池中，从未参与检测
  self_node_id: sn.id,
  enabled: false
})

// buildUnifiedRows 合成统一表行:池行(自建行补 id/enabled)+ 禁用自建行。
export const buildUnifiedRows = (poolNodes: Node[], selfNodes: SelfNode[]): UnifiedNode[] => {
  const byIdentity = new Map<string, SelfNode>()
  for (const sn of selfNodes) {
    byIdentity.set(selfIdentity(sn.server, sn.port, sn.protocol), sn)
  }

  const rows: UnifiedNode[] = poolNodes.map((n) => {
    if (n.source !== SELF_HOSTED) return n
    const sn = byIdentity.get(selfIdentity(n.server, n.port, n.type))
    return { ...n, self_node_id: sn?.id, enabled: true } // 在池中即为启用
  })

  const disabled = selfNodes.filter((sn) => !sn.enabled).map(selfNodeToRow)
  return [...rows, ...disabled]
}

// selfNodeIndex 按 id 建立 SelfNode 索引,供内联编辑预填完整配置。
export const selfNodeIndex = (selfNodes: SelfNode[]): Map<number, SelfNode> => {
  const m = new Map<number, SelfNode>()
  for (const sn of selfNodes) m.set(sn.id, sn)
  return m
}
