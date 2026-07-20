package subscription

import (
	"fmt"
	"time"
)

// SourceSelfHosted 自建节点的来源标记（FailBack 安全网，豁免关键词过滤，见 ADR 0001/0005）
const SourceSelfHosted = "self-hosted"

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

	// 网络配置
	Network         string `json:"network,omitempty"` // tcp, ws, grpc
	TLS             bool   `json:"tls,omitempty"`
	SNI             string `json:"sni,omitempty"`               // TLS SNI / servername（anytls 等依赖）
	Insecure        bool   `json:"insecure,omitempty"`          // 跳过证书校验（对应订阅里的 insecure=1）
	GrpcServiceName string `json:"grpc_service_name,omitempty"` // gRPC service name (vless/vmess over grpc)

	// 元数据
	Region string `json:"region"` // 地区：香港、日本、美国等
	Source string `json:"source"` // 来源机场名称

	// DisplayName 订阅生成时计算的标准化展示名（见 ADR 0012）。
	// 空表示未标准化，下发时回退用 Name（机场原名）。nodes 表不持久化此字段。
	DisplayName string `json:"display_name,omitempty"`

	// 健康状态
	Available bool      `json:"available"`
	Latency   int       `json:"latency"` // 延迟（毫秒）
	LastCheck time.Time `json:"last_check"`

	// 真实检测时间戳(区分 TCP 快检 vs 真实代理检测)
	DetectionLastCheck time.Time `json:"detection_last_check,omitempty"`

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
