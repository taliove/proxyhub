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
  // 节点范围筛选条件的原始 JSON(见 internal/subfilter.Conditions);''=不筛选=全量
  conditions: string
  // 配置模板名称(软引用);''=跟随默认模板(见模板库四级回退链)
  template_name: string
  // 虚拟状态节点开关(issue #102):true = 订阅第一位注入节点状态摘要哑节点
  status_node_enabled?: boolean
  // 槽位模式:true = 只下发有槽位挂载的节点(精选/节点范围/关键词不生效)
  slot_mode?: boolean
  // 地域白名单(pull-guard ticket 07):'off'=不判(默认), 'observe'=只留痕仍下发,
  // 'enforce'=不匹配则 403。两个列表逗号分隔,空=该维度不判(所以 enforce + 双空仍全放行)。
  // 省份维度受内置库限制:当前内置库只有国家级数据,省份列表非空即恒不匹配。
  geo_mode?: 'off' | 'observe' | 'enforce'
  geo_countries?: string
  geo_provinces?: string
  // 订阅 profile 公开名称(issue #38):非空时客户端显示「ProxyHub · <公开名>」,
  // 空串=未设=裸品牌名;与私有 alias(绝不下发)相对。
  public_name: string
  // 链接重置宽限(issue #117):旧链接的最后可用时刻(UTC 文本);'' = 无宽限
  grace_expires_at?: string
  // 精选节点集(issue #80,后端 issue #79):NodeKey 数组的 JSON 字符串;
  // ''=未配置=全量,解析失败按空(与后端 endpointNodePicks 降级语义一致)。
  // NodeKey = server:port(节点 SNI 非空时 server:port:sni),改名仍命中、下架自然失效。
  node_picks?: string
  // 会下发集合的可用性汇总(列表接口加性附加,池状态实时算,见 ADR 0028 决策 2)
  availability?: { available: number; total: number }
}

// SubscriptionConditions 订阅地址的节点范围筛选条件(与 Go 侧 subfilter.Conditions 对齐)。
// 机场/地区/关键词各维度跨维度 AND;机场、地区维度内 OR;标签维度内 AND(全含才命中)。
export interface SubscriptionConditions {
  airports: string[]
  regions: string[]
  tags: string[]
  keyword: string
}

// NodePick 订阅地址精选项(issue #85 对象形态,与 Go 侧 store.NodePick 对齐):
// key = NodeKey;alias 可选,非空时是该订阅下发命名链的最终层(仅本订阅生效),
// 留空 = 跟随命名链(标准化/改名覆盖 → 订阅级模板)。
export interface NodePick {
  key: string
  alias?: string
}

export interface Airport {
  id: number
  name: string
  url: string
  abbr: string
  enabled: boolean
  created_at: string
  // 来源类型(CONTEXT.md「手动机场」):url = 拉取型(默认) / manual = 手动机场(粘贴导入,url 恒为空串)
  source_type?: 'url' | 'manual'
  // 用量信息(CONTEXT.md「用量信息」;全部可选,0/空 = 未知不展示):
  // upload/download/total 单位字节,expire 为 unix 秒(0 = 未知)
  usage_upload?: number
  usage_download?: number
  usage_total?: number
  usage_expire?: number
  web_page_url?: string
  last_test_score?: number | null
  last_test_at?: string | null
  last_test_status?: string | null
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
  // 节点收藏(issue #83):展示层星标,服务端持久(node_overrides.favorite),
  // 不参与订阅过滤链;老后端/未透出时缺省,按未收藏处理
  favorite?: boolean
  stale: boolean // 机场订阅中已消失的节点
  // 排障用协议参数(ticket 0016;uuid/password 属凭证,后端不透出)
  cipher?: string
  alter_id?: number
  plugin?: string // SS 插件（simple-obfs/v2ray-plugin）
  plugin_opts?: string // 插件参数原始串（"obfs=http;obfs-host=x"）
  grpc_service_name?: string
  grpc_authority?: string // gRPC Host(mihomo 读 servername;spec #72)
  insecure?: boolean // 跳过证书校验（订阅里的 insecure=1）
  // 可用性判定来源(与后端 subscription.AvailabilitySource* 对齐):
  // never=从未检测 / health=仅健康检查(TCP 快检)/ real=真实代理检测
  availability_source: 'never' | 'health' | 'real'
  detection_last_check?: string // 最近一次检测时间（RFC3339;从未检测时缺省）
  // 最近检测失败原因(ticket 0017;与后端 detection.FailReason* 对齐):
  // 分类为有限枚举,详情为截断短文本;检测成功/从未检测时缺省
  detection_fail_reason?: 'timeout' | 'refused' | 'unreachable' | 'handshake' | 'protocol' | 'other'
  detection_fail_detail?: string
  unlock_results?: Record<string, UnlockResult> // 多维解锁检测结果
  bandwidth_down_mbps?: number // 最近一次带宽测试下行
  bandwidth_up_mbps?: number // 最近一次带宽测试上行
  tags?: string[] // 自动标签（见票据 21;后端透出前缺省，按空态处理）
  stability_score?: number // 最近体检稳定性分 0..100(见票据 54;无体检记录时缺省，按无分处理)
  share_link?: string // 节点分享链接（机场节点从订阅解析时保留原始链接）
}

export interface UnlockResult {
  available: boolean
  latency: number
  error?: string
  down_mbps?: number
  up_mbps?: number
  level?: string // 解锁级别：full/originals_only/blocked(仅专用解锁判定填充)
  region?: string // 命中区域国家码（如 US/HK）,空则不展示
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

// /api/nodes 查询参数(见 ADR 0013;keyword 名称/地区搜索见 ticket 0041)
export interface NodeListParams {
  page?: number
  page_size?: number
  source?: string // 机场名，子串模糊匹配
  keyword?: string // 名称片段或地区（地区码/地区中文名）
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
  grpc_authority?: string
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

// 直连出口(Direct Egress)设置项(见 CONTEXT.md「直连出口」;ticket 0019/0021)。
// 后端 /api/settings 为 map[string]string 契约,三个键均为字符串;enabled 取 'true'/'false'。
export interface DirectEgressSettings {
  // 'true'=开启(默认):检测连接经自带 DoH 解析 + 绑定物理网卡;'false'=恢复系统网络栈
  direct_egress_enabled: string
  // DoH 端点,须 http(s) URL 且 host 为 IP 字面量;留空用默认 https://223.5.5.5/dns-query
  direct_egress_doh_url: string
  // 物理网卡名;留空=自动识别
  direct_egress_interface: string
}

export interface RefreshRun {
  id: number
  trigger: string // manual | scheduled | startup
  status: string // running | success | partial | failed | cancelled
  job_id: number // 关联的 jobs 任务 id(0 = 任务化前的旧记录)
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

// 每机场结构化拉取诊断(ticket 0018);http_status=0 表示网络错误未拿到响应
export interface RefreshFetchDiag {
  id: number
  run_id: number
  airport: string
  airport_id: number
  http_status: number
  duration_ms: number
  node_count: number
  parse_failures: number
  error: string
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

// 深度体检 - 单区测速结果(成功含 TTFB、下行速率与上行速率,失败仅 error)
// 下行失败则不测上行(up_mbps 为 0);下行成功但上行失败则 down_mbps 正常,error 标记上行问题
export interface ExamRegionResult {
  code: string
  name: string
  ttfb_ms: number
  down_mbps: number
  up_mbps?: number // 上行速率（全区填充；上行失败时为 0 或缺失）
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
  region?: string // 命中区域国家码（如 US/HK）
}

// 深度体检 - 解锁段聚合结果(逐目标一条,顺序与 DefaultUnlockTargets 一致)
export interface ExamUnlockMetrics {
  results: ExamUnlockResult[]
}

// 深度体检 - IPv4 出口信息(成功含地址/地区/ASN/标记,失败仅 error)
export interface ExamEgressIPv4 {
  ip?: string
  country?: string
  country_code?: string
  region?: string
  city?: string
  asn?: string
  org?: string
  proxy: boolean // 疑似代理/VPN 出口
  hosting: boolean // 机房/数据中心 IP(非住宅)
  error?: string
}

// 深度体检 - IPv6 出口信息(available 有出口给地址;不可达 available=false 且无 error;解析失败带 error)
export interface ExamEgressIPv6 {
  available: boolean
  address?: string
  error?: string
}

// 深度体检 - 出口 DNS 信息(解析器 IP/归属地,leak 为疑似 DNS 泄露)
export interface ExamEgressDNS {
  resolver_ip?: string
  resolver_geo?: string
  leak: boolean
  error?: string
}

// 深度体检 - 出网信息段聚合结果(IPv4/IPv6/DNS 各一份)
export interface ExamEgressMetrics {
  ipv4?: ExamEgressIPv4
  ipv6?: ExamEgressIPv6
  dns?: ExamEgressDNS
}

// 深度体检报告(稳定性段 + 多地域测速段 + 解锁段 + 出网信息段)
export interface ExamReport {
  stability?: ExamStabilityMetrics
  region_speed?: ExamRegionSpeedMetrics
  unlock?: ExamUnlockMetrics
  egress?: ExamEgressMetrics
}

// 深度体检历史记录(后端 store.ExamHistoryEntry;report 已从 JSON 解析)
export interface ExamHistoryEntry {
  id: number
  node_key: string
  report: ExamReport
  // job_id 产出本记录的 jobs 任务 id(ticket 0022;0/缺省 = 任务结果关联前的旧数据)
  job_id?: number
  created_at: string // RFC3339
}

// 深度体检 SSE 事件帧
export interface ExamEvent {
  phase: 'sample' | 'region' | 'unlock' | 'egress' | 'section_done' | 'done' | 'error' | 'cancelled'
  // seq 顶层单调序号:附加已有任务时服务端先回放(带 seq)再直播,前端凭此去重实现无感续传。
  seq?: number
  section?: string
  sample?: ExamStabilitySample
  metrics?: ExamStabilityMetrics
  region?: ExamRegionResult
  region_speed?: ExamRegionSpeedMetrics
  unlock_result?: ExamUnlockResult
  unlock?: ExamUnlockMetrics
  egress?: ExamEgressMetrics
  report?: ExamReport
  error?: string
}
