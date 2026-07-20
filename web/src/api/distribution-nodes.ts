import client from './client'

export interface DistributionNode {
  id: number
  name: string
  path: string
  upstream_node_keys: string[]
  lb_strategy: string
  total_upload: number
  total_download: number
  total_connections: number
  last_access: string
  enabled: boolean
  created_at: string
}

export interface CreateDistributionNodeRequest {
  name: string
  path: string
  upstream_node_keys: string[]
  lb_strategy: string
  enabled?: boolean
}

export interface UpdateDistributionNodeRequest {
  name: string
  path: string
  upstream_node_keys: string[]
  lb_strategy: string
  enabled?: boolean
}

export const listDistributionNodes = async (): Promise<DistributionNode[]> => {
  return await client.get<any, DistributionNode[]>('/distribution/paths')
}

export const getDistributionNode = async (id: number): Promise<DistributionNode> => {
  return await client.get<any, DistributionNode>(`/distribution/paths/${id}`)
}

export const createDistributionNode = async (
  data: CreateDistributionNodeRequest
): Promise<DistributionNode> => {
  return await client.post<any, DistributionNode>('/distribution/paths', data)
}

export const updateDistributionNode = async (
  id: number,
  data: UpdateDistributionNodeRequest
): Promise<void> => {
  await client.put(`/distribution/paths/${id}`, data)
}

export const deleteDistributionNode = async (id: number): Promise<void> => {
  await client.delete(`/distribution/paths/${id}`)
}

export const toggleDistributionNode = async (
  id: number
): Promise<{ ok: boolean; enabled: boolean }> => {
  return await client.post<any, { ok: boolean; enabled: boolean }>(
    `/distribution/paths/${id}/toggle`,
    {}
  )
}
