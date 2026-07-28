// 系统状态(初始化标记 + 版本自报),对应 GET /api/status,无需认证
import client from './client'

export interface SystemStatus {
  initialized: boolean
  version?: string
  build_time?: string
}

export const getStatus = (): Promise<SystemStatus> => client.get('/status')
