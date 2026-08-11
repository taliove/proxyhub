import client from './client'

// 名称槽位 API(ADR 0047 / issue #97 #98):用户级统一命名层。
// 命名链:精选 alias > 槽位名 > 模板标准化 > 原名;改名/转移下一次订阅请求即生效。

export interface SlotNodeSummary {
  name: string
  source: string
  region: string
  available: boolean
  latency: number
  stale: boolean
  missing: boolean
  // 最近一次监控探测(issue #103);无监控数据时缺省
  last_probe_at?: string
  last_probe_ok?: boolean
}

export interface NameSlot {
  name: string
  node_key: string
  empty: boolean
  created_at: string
  updated_at: string
  node?: SlotNodeSummary
  // 渲染后实际名称(含变量按挂载节点渲染);未指派/无变量时缺省,回退展示 name
  display?: string
  // 24 小时探测网格(issue #103):24 格,旧→新;0=无数据 1=全通 2=部分通 3=全断
  probe_grid?: number[]
}

// 迁移落选冲突行(display_name 残留待人工处理)
export interface SlotConflictRow {
  node_key: string
  display_name: string
  region: string
  favorite: boolean
  updated_at: string
}

export interface SlotListResponse {
  slots: NameSlot[]
  conflicts: SlotConflictRow[]
  // 监控总开关(订阅节点监控):关时前端提示"监控未开启"而非空数据
  monitor_enabled?: boolean
}

// 409 冲突载荷(与后端 writeSlotError 对齐)
export interface SlotConflict {
  kind: 'name_taken' | 'node_occupied' | 'reassign' | 'concurrent'
  name?: string
  node_key?: string
  holder_name?: string
  holder_node_key?: string
}

// readSlotConflict 从 axios 错误中提取 409 冲突载荷;非冲突返回 null
export const readSlotConflict = (e: unknown): SlotConflict | null => {
  const resp = (e as { response?: { status?: number; data?: { conflict?: SlotConflict } } })
    ?.response
  if (resp?.status === 409 && resp.data?.conflict) return resp.data.conflict
  return null
}

export const listSlots = (): Promise<SlotListResponse> => client.get('/slots')

export const createSlot = (name: string, nodeKey = '', force = false): Promise<unknown> =>
  client.post('/slots', { name, node_key: nodeKey, force })

// updateSlot nodeKey 语义:undefined = 不变;'' = 摘下变空槽;非空 = 指派/转移
export const updateSlot = (
  name: string,
  opts: { newName?: string; nodeKey?: string; force?: boolean }
): Promise<unknown> =>
  client.put(`/slots/${encodeURIComponent(name)}`, {
    new_name: opts.newName,
    node_key: opts.nodeKey,
    force: opts.force ?? false
  })

export const deleteSlot = (name: string): Promise<unknown> =>
  client.delete(`/slots/${encodeURIComponent(name)}`)

// previewSlotName 槽位名模板实时预览:按挂载节点渲染出订阅实际显示名
// (与生成链同一 Standardizer);无变量/无节点时 resolved=false、原样返回
export const previewSlotName = (
  name: string,
  nodeKey = ''
): Promise<{ rendered: string; resolved: boolean }> =>
  client.post('/slots/preview-name', { name, node_key: nodeKey })
