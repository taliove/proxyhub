package generator

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// uniqueName 保证节点名称唯一（Clash 要求）
func uniqueName(name string, seen map[string]int) string {
	if name == "" {
		name = "节点"
	}
	count := seen[name]
	seen[name] = count + 1
	if count == 0 {
		return name
	}
	return fmt.Sprintf("%s-%d", name, count+1)
}

// clashProxy 将节点转换为 Clash proxy 配置
// ClashProxy 将 Node 转换为 Clash 配置 map(供订阅生成和检测使用)
func ClashProxy(node *subscription.Node, name string) (map[string]any, error) {
	base := map[string]any{
		"name":   name,
		"server": node.Server,
		"port":   node.Port,
	}

	switch node.Type {
	case "vmess":
		base["type"] = "vmess"
		base["uuid"] = node.UUID
		base["alterId"] = node.AlterID
		cipher := node.Cipher
		if cipher == "" {
			cipher = "auto"
		}
		base["cipher"] = cipher
		if node.Network != "" && node.Network != "tcp" {
			base["network"] = node.Network
		}
		if node.TLS {
			base["tls"] = true
		}
	case "vless":
		base["type"] = "vless"
		base["uuid"] = node.UUID
		if node.Network != "" && node.Network != "tcp" {
			base["network"] = node.Network
		}
		if node.TLS {
			base["tls"] = true
		}
		// gRPC 传输选项
		if node.Network == "grpc" && node.GrpcServiceName != "" {
			base["grpc-opts"] = map[string]any{
				"grpc-service-name": node.GrpcServiceName,
			}
		}
	case "trojan":
		base["type"] = "trojan"
		base["password"] = node.Password
		if node.SNI != "" {
			base["sni"] = node.SNI
		}
		if node.Insecure {
			base["skip-cert-verify"] = true
		}
	case "anytls":
		// AnyTLS 由 Clash.Meta/mihomo 支持；password 承载凭据，始终基于 TLS
		base["type"] = "anytls"
		base["password"] = node.Password
		if node.SNI != "" {
			base["sni"] = node.SNI
		}
		if node.Insecure {
			base["skip-cert-verify"] = true
		}
	case "ss":
		base["type"] = "ss"
		base["cipher"] = node.Cipher
		base["password"] = node.Password
	default:
		return nil, fmt.Errorf("unsupported type: %s", node.Type)
	}

	return base, nil
}
