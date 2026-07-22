import client from '@/api/client'

// 订阅测试(见 CONTEXT.md「订阅测试」与 ADR 0028):即时诊断,不落库;
// 拉取验证走内部生成链不记 pull_logs,现场实测 run 只存内存(重启/过期即失,404 需容错)。

// 单格式拉取验证结果(与后端 formatCheckResult 对齐)
export interface FormatCheckResult {
  valid: boolean
  node_count: number
  duration_ms: number
  error?: string
}

// 会下发节点的池状态快照(与后端 poolSnapshotView 对齐)
export interface PoolSnapshot {
  total: number
  available: number
  mean_latency_ms: number
  region_count: number
  regions: string[]
}

// 拉取验证(Clash/V2Ray 双格式)+ 池快照
export interface EndpointTestResult {
  pull: { clash: FormatCheckResult; v2ray: FormatCheckResult }
  snapshot: PoolSnapshot
}

// 现场实测 run(与后端 endpointProbeRun 对齐);状态字符串与机场测试 run 对齐
export type ProbeRunStatus = 'running' | 'completed' | 'failed'

export interface ProbeRun {
  run_id: string
  endpoint_id: number
  full: boolean
  status: ProbeRunStatus
  total: number // 会下发节点数
  sampled: number // 抽样后实际检活数(全量时等于 total)
  checked: number
  error?: string
}

// 拉取验证 + 池快照:POST 即算即返,不产生实测
export async function runEndpointTest(endpointId: number): Promise<EndpointTestResult> {
  return client.post<unknown, EndpointTestResult>(`/endpoints/${endpointId}/test`)
}

// 发起现场实测:full=false 地区分层抽样,full=true 测全部;立即返回 run 句柄
export async function startEndpointProbe(endpointId: number, full: boolean): Promise<ProbeRun> {
  return client.post<unknown, ProbeRun>(`/endpoints/${endpointId}/test/probe`, { full })
}

// 轮询实测进度;run 不存在(重启丢失/TTL 过期/其他端点)后端返回 404
export async function getEndpointProbe(endpointId: number, runId: string): Promise<ProbeRun> {
  return client.get<unknown, ProbeRun>(`/endpoints/${endpointId}/test/probe/${runId}`)
}

// 实测进度分母:已回报抽样数按抽样数,未回报前按会下发总数。
// Pure function for easy testing.
export function probeTotalOf(run: ProbeRun): number {
  return run.sampled > 0 ? run.sampled : run.total
}

// 判定错误是否为 404(实测 run 丢失的容错分支)。
// Pure function for easy testing.
export function isNotFound(err: unknown): boolean {
  return (err as { response?: { status?: number } } | null)?.response?.status === 404
}
