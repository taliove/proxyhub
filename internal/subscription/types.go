package subscription

import (
	"fmt"
	"time"
)

// SourceSelfHosted 自建节点的来源标记（FailBack 安全网，豁免关键词过滤，见 ADR 0001/0005）
const SourceSelfHosted = "self-hosted"

// 可用性判定来源(见 ticket 0016):当前 Available 值由哪类检测最近一次写下。
// 持久化在 nodes.detection_kind 列,随 MergePool carry-forward 跨刷新保留。
const (
	// DetectionKindHealth 仅健康检查(TCP 快检):聚合健康检查、quick 即时测试、机场测试抽样检活。
	DetectionKindHealth = "health"
	// DetectionKindReal 真实代理检测(real 即时测试:构 mihomo adapter 经代理请求)。
	DetectionKindReal = "real"
)

// 可用性判定来源的对外枚举(nodeView.availability_source / 前端展示文案)。
const (
	AvailabilitySourceNever  = "never"  // 从未检测(如单机场刷新新入池,未跑任何检查)
	AvailabilitySourceHealth = "health" // 仅健康检查(TCP 快检)
	AvailabilitySourceReal   = "real"   // 真实代理检测
)

// Node 代理节点
type Node struct {
	// 基本信息
	Name   string `json:"name"`
	Type   string `json:"type"` // vmess, vless, trojan, ss, hysteria2
	Server string `json:"server"`
	Port   int    `json:"port"`

	// 协议特定参数
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	AlterID  int    `json:"alter_id,omitempty"`
	Cipher   string `json:"cipher,omitempty"`

	// SS 插件(SIP002):simple-obfs / v2ray-plugin 等。机场节点常靠 obfs 混淆才能连通,
	// 丢失会导致重建的订阅全部不可用;PluginOpts 保留 "obfs=http;obfs-host=x" 原始串。
	Plugin     string `json:"plugin,omitempty"`
	PluginOpts string `json:"plugin_opts,omitempty"`

	// 网络配置
	Network         string `json:"network,omitempty"` // tcp, ws, grpc
	TLS             bool   `json:"tls,omitempty"`
	SNI             string `json:"sni,omitempty"`               // TLS SNI / servername（anytls 等依赖）
	Insecure        bool   `json:"insecure,omitempty"`          // 跳过证书校验（对应订阅里的 insecure=1）
	GrpcServiceName string `json:"grpc_service_name,omitempty"` // gRPC service name (vless/vmess over grpc)

	// 元数据
	Region string `json:"region"` // 地区：香港、日本、美国等
	Source string `json:"source"` // 来源机场名称
	// UserID 节点属主(ticket 07):机场节点=机场属主,自建节点=自建节点属主;
	// 0 = 未归属(旧快照/单管理员合并前)。内存池按此分片,DB nodes 表不重复存
	// (属主从 airports/self_hosted_nodes 推导,快照持久化时随池写入)。
	UserID int64 `json:"user_id,omitempty"`

	// DisplayName 订阅生成时计算的标准化展示名（见 ADR 0012）。
	// 空表示未标准化，下发时回退用 Name（机场原名）。nodes 表不持久化此字段。
	DisplayName string `json:"display_name,omitempty"`

	// 健康状态
	Available bool      `json:"available"`
	Latency   int       `json:"latency"` // 延迟（毫秒）
	LastCheck time.Time `json:"last_check"`

	// 真实检测时间戳(区分 TCP 快检 vs 真实代理检测)
	DetectionLastCheck time.Time `json:"detection_last_check,omitempty"`

	// DetectionKind 当前 Available 判定所依据的最近检测类型(DetectionKindHealth/DetectionKindReal,
	// 空 = 从未检测)。注意 DetectionLastCheck 无法担此任:quick(TCP)与 real(代理)即时测试都会
	// 写 DetectionLastCheck,单靠时间戳区分不了快检与真实检测,故需此最小标记(见 ticket 0016)。
	// 语义为"最近一次写下 Available 的检测类型",如实跟随 quick/real 覆盖,不做单调升级。
	DetectionKind string `json:"detection_kind,omitempty"`

	// DetectionFailReason 最近一次检测失败的原因分类(detection.FailReason* 枚举,
	// 空 = 最近检测成功或从未检测)。与 DetectionKind 同生命周期:检测写回时失败填分类、
	// 成功清空;随 MergePool carry-forward 跨刷新保留(见 ticket 0017)。
	DetectionFailReason string `json:"detection_fail_reason,omitempty"`
	// DetectionFailDetail 失败短详情(截断到 detection.MaxFailDetailLen,不含凭证;
	// 可能含 server:port 等非敏感排障信息)。分类是主信号,详情仅辅助。
	DetectionFailDetail string `json:"detection_fail_detail,omitempty"`

	// RawLink 保留订阅解析时的原始分享 URI(含凭证)。仅供 share-uri 端点按原样
	// 回放节点二维码,避免走生成器重造丢失机场特有参数(见 ticket 56)。
	// json:"-" 确保它绝不出现在 /nodes 视图或任何 JSON 序列化输出;同理禁止写入日志。
	// 解析失败的节点不产生 Node,故此字段对它们天然为空。不持久化到 nodes 表
	// (与 DisplayName 同类,仅进程内有效;重启后下一轮刷新重新填充)。
	RawLink string `json:"-"`

	// Stale 标记节点从机场订阅中消失(保留待清理,订阅生成时排除)
	Stale bool `json:"stale,omitempty"`
	// LastSeen 最近一次在 fetch 中出现的时间(stale=true 时有意义)
	LastSeen time.Time `json:"last_seen,omitempty"`

	// 带宽测试结果(最近一次)
	BandwidthDownMbps float64   `json:"bandwidth_down_mbps,omitempty"`
	BandwidthUpMbps   float64   `json:"bandwidth_up_mbps,omitempty"`
	BandwidthCheck    time.Time `json:"bandwidth_check,omitempty"`
}

// NodeKey 返回节点的唯一标识（用于去重与屏蔽名单）。
//
// 基础键为 server:port。但部分机场（如 anytls）把多个逻辑节点复用同一 server:port，
// 仅靠 SNI 区分（例：香港01/香港02 都是 :28537，SNI 不同）。此时把 SNI 并入键，
// 避免去重把它们误合并为一个。SNI 为空时退化为 server:port，与历史键保持兼容
// （既有 node_blocks 名单不失效）。
func (n *Node) NodeKey() string {
	if n.SNI != "" {
		return fmt.Sprintf("%s:%d:%s", n.Server, n.Port, n.SNI)
	}
	return fmt.Sprintf("%s:%d", n.Server, n.Port)
}

// AvailabilitySource 返回可用性判定来源的对外枚举(never/health/real)。
// 未知/空值一律归并为 never,保证全池口径一致、前端只处理三态。
func (n *Node) AvailabilitySource() string {
	switch n.DetectionKind {
	case DetectionKindHealth:
		return AvailabilitySourceHealth
	case DetectionKindReal:
		return AvailabilitySourceReal
	default:
		return AvailabilitySourceNever
	}
}

// EffectiveName 返回下发订阅时应使用的节点名:标准化名(DisplayName)非空则用它,
// 否则回退机场原名(Name)。生成器统一通过此方法取名,兼容"未启用标准化"场景。
func (n *Node) EffectiveName() string {
	if n.DisplayName != "" {
		return n.DisplayName
	}
	return n.Name
}

// Subscription 订阅信息
type Subscription struct {
	Name  string  // 机场名称
	URL   string  // 订阅 URL
	Nodes []*Node // 节点列表
}

// NodeList 节点列表（用于排序和过滤）
type NodeList []*Node

func (nl NodeList) Len() int           { return len(nl) }
func (nl NodeList) Less(i, j int) bool { return nl[i].Latency < nl[j].Latency }
func (nl NodeList) Swap(i, j int)      { nl[i], nl[j] = nl[j], nl[i] }
