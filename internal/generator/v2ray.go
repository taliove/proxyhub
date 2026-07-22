package generator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// GenerateV2Ray 生成 V2Ray 格式订阅（Base64 编码的分享链接列表）
func GenerateV2Ray(nodes []*subscription.Node) ([]byte, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes to generate")
	}

	var links []string
	for _, node := range nodes {
		link, err := shareLink(node)
		if err != nil {
			// 跳过无法转换的节点
			continue
		}
		links = append(links, link)
	}

	if len(links) == 0 {
		return nil, fmt.Errorf("no convertible nodes")
	}

	content := strings.Join(links, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	return []byte(encoded), nil
}

// shareLink 将节点转换为分享链接
func shareLink(node *subscription.Node) (string, error) {
	switch node.Type {
	case "vmess":
		return vmessLink(node)
	case "vless":
		return vlessLink(node), nil
	case "trojan":
		return trojanLink(node), nil
	case "ss":
		return ssLink(node), nil
	case "anytls":
		return anytlsLink(node), nil
	default:
		return "", fmt.Errorf("unsupported type: %s", node.Type)
	}
}

// GenerateShareURI exports shareLink for API handlers to generate single node URI
func GenerateShareURI(node *subscription.Node) (string, error) {
	return shareLink(node)
}

func vmessLink(node *subscription.Node) (string, error) {
	tls := ""
	if node.TLS {
		tls = "tls"
	}
	network := node.Network
	if network == "" {
		network = "tcp"
	}

	cfg := map[string]any{
		"v":    "2",
		"ps":   node.EffectiveName(),
		"add":  node.Server,
		"port": fmt.Sprintf("%d", node.Port),
		"id":   node.UUID,
		"aid":  fmt.Sprintf("%d", node.AlterID),
		"net":  network,
		"type": "none",
		"host": "",
		"path": "",
		"tls":  tls,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal vmess: %w", err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
}

// escapeFragment 编码分享链接 #fragment(备注):空格必须是 %20 而非 +。
// + 是 query 表单编码约定,fragment 只做 percent-decode,客户端会把 + 原样显示。
func escapeFragment(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// NormalizeShareURIFragment 规范化已有分享链接的 fragment:+ 与 %20 统一解码后
// 按 %20 重编码,其余部分(userinfo/host/query)逐字节保持原样。
// 用于 RawLink 保真回放路径:机场原文的备注编码不一定是 fragment 安全的。
func NormalizeShareURIFragment(uri string) string {
	idx := strings.LastIndex(uri, "#")
	if idx < 0 {
		return uri
	}
	name, err := url.QueryUnescape(uri[idx+1:])
	if err != nil {
		return uri // 非法转义序列:保持原文,不破坏链接
	}
	return uri[:idx+1] + escapeFragment(name)
}

func vlessLink(node *subscription.Node) string {
	params := url.Values{}
	network := node.Network
	if network == "" {
		network = "tcp"
	}
	params.Set("type", network)
	if node.TLS {
		params.Set("security", "tls")
	}
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		node.UUID, node.Server, node.Port, params.Encode(), escapeFragment(node.EffectiveName()))
}

func trojanLink(node *subscription.Node) string {
	return fmt.Sprintf("trojan://%s@%s:%d#%s",
		node.Password, node.Server, node.Port, escapeFragment(node.EffectiveName()))
}

// anytlsLink 还原 anytls 分享链接：anytls://password@server:port?sni=&insecure=1#name
func anytlsLink(node *subscription.Node) string {
	params := url.Values{}
	if node.SNI != "" {
		params.Set("sni", node.SNI)
	}
	if node.Insecure {
		params.Set("insecure", "1")
	}
	suffix := ""
	if enc := params.Encode(); enc != "" {
		suffix = "?" + enc
	}
	return fmt.Sprintf("anytls://%s@%s:%d%s#%s",
		node.Password, node.Server, node.Port, suffix, escapeFragment(node.EffectiveName()))
}

func ssLink(node *subscription.Node) string {
	userInfo := base64.URLEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s:%s", node.Cipher, node.Password)))
	link := fmt.Sprintf("ss://%s@%s:%d", userInfo, node.Server, node.Port)
	// SIP002 plugin 参数原样回写(simple-obfs/v2ray-plugin),丢失会导致节点不可用
	if node.Plugin != "" {
		raw := node.Plugin
		if node.PluginOpts != "" {
			raw += ";" + node.PluginOpts
		}
		link += "/?plugin=" + url.QueryEscape(raw)
	}
	return link + "#" + escapeFragment(node.EffectiveName())
}
