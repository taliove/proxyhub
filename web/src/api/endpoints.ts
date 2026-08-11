// Endpoint (subscription address) API client
import client from './client'
import type { NodePick } from '@/types'

export interface UpdateEndpointTemplateRequest {
  template_name: string // Empty string to unbind (follow default)
}

// 虚拟状态节点开关(issue #102):开启后该地址输出第一位注入监控摘要哑节点
export function setEndpointStatusNode(id: number, enabled: boolean): Promise<{ ok: boolean }> {
  return client.put<unknown, { ok: boolean }>(`/endpoints/${id}/status-node`, { enabled })
}

// 槽位模式开关:开启后只下发有槽位挂载的节点,名字即槽位名
export function setEndpointSlotMode(id: number, enabled: boolean): Promise<{ ok: boolean }> {
  return client.put<unknown, { ok: boolean }>(`/endpoints/${id}/slot-mode`, { enabled })
}

// Update endpoint template binding
export function updateEndpointTemplate(
  id: number,
  templateName: string
): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/endpoints/${id}/template`, {
    template_name: templateName
  })
}

export interface UpdateEndpointGeoConfigRequest {
  geo_mode: string // 'off' | 'observe' | 'enforce'
  geo_countries: string // Comma-separated country codes (e.g., "CN,US")
  geo_provinces: string // Comma-separated province codes/names
}

// Update endpoint geo allowlist configuration (pull-guard ticket 07/08)
export function updateEndpointGeoConfig(
  id: number,
  geoMode: string,
  geoCountries: string,
  geoProvinces: string
): Promise<{ success: boolean }> {
  return client.put<unknown, { success: boolean }>(`/endpoints/${id}/geo-config`, {
    geo_mode: geoMode,
    geo_countries: geoCountries,
    geo_provinces: geoProvinces
  })
}

export interface UpdateEndpointNodePicksRequest {
  node_picks: NodePick[] // 精选项对象数组（issue #85）;空数组 = 清空精选 = 恢复全量（后端零回归语义）
}

// 设置订阅地址精选节点集(issue #80,后端 issue #79;对象形态 issue #85):
// key = NodeKey(server:port,节点 SNI 非空时 server:port:sni),alias 可选
// (该订阅下发命名链最终层,留空 = 跟随命名链);空数组落库为空串 = 未配置。
export function updateEndpointNodePicks(id: number, picks: NodePick[]): Promise<{ ok: boolean }> {
  return client.put<unknown, { ok: boolean }>(`/endpoints/${id}/node-picks`, {
    node_picks: picks
  })
}

export interface UpdateEndpointPublicNameRequest {
  public_name: string // Empty string clears the name (bare brand title)
}

// Update endpoint public name (subscription profile title, issue #38)
export function updateEndpointPublicName(id: number, publicName: string): Promise<{ ok: boolean }> {
  return client.put<unknown, { ok: boolean }>(`/endpoints/${id}/public-name`, {
    public_name: publicName
  })
}
