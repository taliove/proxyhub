import client from './client'

// TypeScript interfaces
export interface DistributionConfig {
  enabled: boolean
  listen_port: number
  domain: string
  protocol: 'vless' | 'vmess'
  network: 'grpc' | 'ws'
  uuid: string
  tls: boolean
  cert_path: string
  key_path: string
}

export interface DistributionPath {
  id: number
  name: string
  path: string
  upstream_node_keys: string[]
  lb_strategy: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface DistributionStat {
  path_id: number
  path_name: string
  upload_bytes: number
  download_bytes: number
  connections: number
  timestamp: string
}

export interface XrayStatus {
  running: boolean
  pid: number
  uptime_seconds: number
  version: string
}

export interface PathStatsParams {
  start_time?: string
  end_time?: string
  interval?: string
}

// API functions
export const getDistributionConfig = () => {
  return client.get<any, DistributionConfig>('/distribution/config')
}

export const updateDistributionConfig = (data: Partial<DistributionConfig>) => {
  return client.put('/distribution/config', data)
}

export const listDistributionPaths = () => {
  return client.get<any, DistributionPath[]>('/distribution/paths')
}

export const getDistributionPath = (id: number) => {
  return client.get<any, DistributionPath>(`/distribution/paths/${id}`)
}

export const createDistributionPath = (data: Partial<DistributionPath>) => {
  return client.post('/distribution/paths', data)
}

export const updateDistributionPath = (id: number, data: Partial<DistributionPath>) => {
  return client.put(`/distribution/paths/${id}`, data)
}

export const deleteDistributionPath = (id: number) => {
  return client.delete(`/distribution/paths/${id}`)
}

export const toggleDistributionPath = (id: number) => {
  return client.post(`/distribution/paths/${id}/toggle`)
}

export const getDistributionStats = (params?: PathStatsParams) => {
  return client.get<any, DistributionStat[]>('/distribution/stats', { params })
}

export const getPathStats = (id: number, params?: PathStatsParams) => {
  return client.get<any, DistributionStat[]>(`/distribution/paths/${id}/stats`, { params })
}

export const restartXray = () => {
  return client.post('/distribution/xray/restart')
}

export const getXrayStatus = () => {
  return client.get<any, XrayStatus>('/distribution/xray/status')
}
