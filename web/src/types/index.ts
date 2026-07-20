export interface Endpoint {
  id: number
  alias: string
  path: string
  token: string
  enabled: boolean
  created_at: string
  // 节点名称标准化按端点覆盖(见 ADR 0012):''=跟随全局, 'on'=强制开, 'off'=强制关
  name_mode: '' | 'on' | 'off'
  name_template: string
}

export interface Airport {
  id: number
  name: string
  url: string
  abbr: string
  enabled: boolean
  created_at: string
}

export interface Node {
  name: string
  display_name: string
  type: string
  server: string
  port: number
  network?: string
  tls: boolean
  sni?: string
  region: string
  source: string
  latency: number
  available: boolean
  node_key: string
  blocked: boolean
  stale: boolean // 机场订阅中已消失的节点
  unlock_results?: Record<string, UnlockResult> // 多维解锁检测结果
  bandwidth_down_mbps?: number // 最近一次带宽测试下行
  bandwidth_up_mbps?: number // 最近一次带宽测试上行
}

export interface UnlockResult {
  available: boolean
  latency: number
  error?: string
  down_mbps?: number
  up_mbps?: number
  level?: string // 解锁级别:full/originals_only/blocked(仅专用解锁判定填充)
  region?: string // 命中区域国家码(如 US/HK),空则不展示
}

// 节点分页查询响应（见 ADR 0013）
export interface NodePage {
  nodes: Node[]
  total: number
  page: number
  page_size: number
  total_pages: number
  last_update: string
}

export interface RegionOption {
  code: string
  name: string
}

export interface SelfNode {
  id: number
  name: string
  protocol: string // ss | trojan | vmess | vless
  server: string
  port: number
  uuid: string
  password: string
  cipher: string
  alter_id: number
  network: string
  tls: boolean
  grpc_service_name: string
  enabled: boolean
}

export interface SystemSettings {
  initialized: boolean
  admin_user?: string
  security?: {
    ban_threshold: number
    ban_duration: string
  }
  alert?: {
    feishu_webhook: string
    min_available_nodes: number
  }
}

export interface RefreshRun {
  id: number
  trigger: string // manual | scheduled | startup
  status: string // running | success | partial | failed
  total_nodes: number
  available_nodes: number
  final_nodes: number
  error: string
  started_at: string
  finished_at: string | null
}

export interface RefreshEvent {
  id: number
  run_id: number
  level: string // info | warn | error
  stage: string // fetch | check | filter | done
  message: string
  data: string // JSON 字符串，可能为空
  created_at: string
}

export interface NodeTestResult {
  available: boolean
  latency: number
  mode: string
  error?: string
  down_mbps?: number
  up_mbps?: number
  elapsed_ms?: number
  min_down_mbps?: number
  min_up_mbps?: number
}

// 带宽测试 SSE 采样帧
export interface BandwidthSample {
  phase: 'download' | 'upload'
  mbps: number
  elapsed_ms: number
}

// 深度体检 - 稳定性采样点
export interface ExamStabilitySample {
  seq: number
  elapsed_ms: number
  latency_ms: number
  ok: boolean
}

// 深度体检 - 稳定性段聚合指标
export interface ExamStabilityMetrics {
  total: number
  succeeded: number
  loss_rate: number // 0..1
  mean_ms: number
  median_ms: number
  p95_ms: number
  p99_ms: number
  jitter_ms: number
  score: number // 0..100
}

// 深度体检 - 单区测速结果(成功含 TTFB 与下行速率,失败仅 error)
export interface ExamRegionResult {
  code: string
  name: string
  ttfb_ms: number
  down_mbps: number
  error?: string
}

// 深度体检 - 多地域测速段聚合结果(逐区一行)
export interface ExamRegionSpeedMetrics {
  regions: ExamRegionResult[]
}

// 深度体检 - 单目标解锁判定结果(复用后端 detection.Result;level/region 仅专用判定填充)
export interface ExamUnlockResult {
  node_key: string
  target_name: string
  available: boolean
  latency: number
  error?: string
  level?: string // full | originals_only | blocked
  region?: string // 命中区域国家码(如 US/HK)
}

// 深度体检 - 解锁段聚合结果(逐目标一条,顺序与 DefaultUnlockTargets 一致)
export interface ExamUnlockMetrics {
  results: ExamUnlockResult[]
}

// 深度体检报告(稳定性段 + 多地域测速段 + 解锁段)
export interface ExamReport {
  stability?: ExamStabilityMetrics
  region_speed?: ExamRegionSpeedMetrics
  unlock?: ExamUnlockMetrics
}

// 深度体检 SSE 事件帧
export interface ExamEvent {
  phase: 'sample' | 'region' | 'unlock' | 'section_done' | 'done' | 'error'
  section?: string
  sample?: ExamStabilitySample
  metrics?: ExamStabilityMetrics
  region?: ExamRegionResult
  region_speed?: ExamRegionSpeedMetrics
  unlock_result?: ExamUnlockResult
  unlock?: ExamUnlockMetrics
  report?: ExamReport
  error?: string
}
