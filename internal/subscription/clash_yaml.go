package subscription

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// 本文件实现 spec #64(issue #65)的 Clash YAML 内容嗅探与 proxies 解析。
// 嗅探与 query 参数/Content-Type/文件名无关:DecodeSubscription 之后、行解析之前,
// 内容按 YAML 解析为顶层 map 且 proxies 键为非空列表即命中 YAML 模式;base64 链接
// 列表在该探测下是 yaml 标量(非 map),天然落空回落行解析老路。字段只映射 Node
// 模型已有字段,未知键静默忽略,未知 type 跳过并计入解析失败(与行解析容错语义一致)。

// parseClashYAML 尝试将订阅内容按 Clash YAML 解析;ok=false 表示嗅探未命中,
// 调用方须回落既有行解析(逐字节零回归)。
//
// 用 yaml.Node 解码而非 map:节点自带源行号,失败明细的 LineFailure.Line
// 才能守住"原文 1 起始行号"的字段契约(手动粘贴入口前端按编辑器行号展示)。
func parseClashYAML(content, source string) (*ParseResult, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, false // 标量/非法 YAML:链接列表形态,走老路
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false // 顶层不是 map(链接列表是标量):不命中
	}
	root := doc.Content[0]

	var proxiesNode *yaml.Node
	var hostsNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "proxies":
			proxiesNode = root.Content[i+1]
		case "hosts":
			hostsNode = root.Content[i+1]
		}
	}
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode || len(proxiesNode.Content) == 0 {
		return nil, false // 无 proxies 键或空列表:不命中
	}

	result := &ParseResult{}
	result.Hosts = parseClashHosts(hostsNode, result)
	for _, item := range proxiesNode.Content {
		// 失败计数口径与行解析一致;Line 取 YAML 节点的真实源行号
		result.TotalLines++
		var m map[string]any
		if err := item.Decode(&m); err != nil || m == nil {
			result.addClashFailure(item.Line, "proxy item is not a map")
			continue
		}
		node, err := clashProxyToNode(m, source)
		if err != nil {
			result.addClashFailure(item.Line, err.Error())
			continue
		}
		// 元数据伪节点过滤复用行解析同一管道(isMetadataName),不发明第二套
		if isMetadataName(node.Name) {
			continue
		}
		// YAML 来源节点没有原始分享链接,RawLink 保持空(spec #64 决策:
		// share-uri 端点走既有回退重建)。去重由调用方 DedupeByNodeKey 管道完成。
		result.Nodes = append(result.Nodes, node)
	}
	return result, true
}

// addClashFailure 计入一次解析失败,明细条数上限与行解析同(maxLineFailures)。
func (r *ParseResult) addClashFailure(line int, reason string) {
	r.ParseFailures++
	if len(r.Failures) < maxLineFailures {
		r.Failures = append(r.Failures, LineFailure{Line: line, Reason: reason})
	}
}

// parseClashHosts 捕获上游顶层 hosts 映射(issue #116):标量键→标量值原样收集。
// hosts 缺失返回 nil;不是 map 或条目非标量时计入解析失败(与 proxies 容错口径
// 一致),不阻断导入。值统一按标量文本取(域名或 IP 字符串)。
// 条目数封顶 maxClashHosts:恶意/故障上游的单次拉取不应膨胀机场行与每次渲染
// 的合并成本(pre-push L2),溢出计一次解析失败并截断。
func parseClashHosts(node *yaml.Node, result *ParseResult) map[string]string {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		result.addClashFailure(node.Line, "hosts is not a map")
		return nil
	}
	hosts := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if len(hosts) >= maxClashHosts {
			result.addClashFailure(node.Line, "hosts exceeds entry limit")
			break
		}
		k, v := node.Content[i], node.Content[i+1]
		if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
			result.addClashFailure(k.Line, "hosts entry is not a scalar mapping")
			continue
		}
		hosts[k.Value] = v.Value
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

// maxClashHosts 上游 hosts 条目数上限(pre-push L2)。
const maxClashHosts = 256

// clashProxyToNode 将单个 Clash proxy map 映射为 Node(只落模型已有字段,spec #64 映射表)。
func clashProxyToNode(m map[string]any, source string) (*Node, error) {
	typ := clashString(m, "type")
	switch typ {
	case "vmess", "vless", "trojan", "ss", "anytls":
		// 支持的协议
	default:
		return nil, fmt.Errorf("unsupported protocol %q", typ)
	}

	server := clashString(m, "server")
	port := clashInt(m["port"])
	if server == "" || port <= 0 {
		return nil, fmt.Errorf("invalid server or port")
	}

	node := &Node{
		Name:   clashString(m, "name"),
		Type:   typ,
		Server: server,
		Port:   port,
		Source: source,
		// 通用传输字段:network/grpc-opts/tls;udp 忽略(模型无此字段)
		Network:         clashString(m, "network"),
		TLS:             clashBool(m["tls"]),
		GrpcServiceName: clashNestedString(m, "grpc-opts", "grpc-service-name"),
	}

	switch typ {
	case "vmess":
		node.UUID = clashString(m, "uuid")
		node.AlterID = clashInt(m["alterId"])
		node.Cipher = clashString(m, "cipher")
	case "vless":
		node.UUID = clashString(m, "uuid")
		node.Flow = clashString(m, "flow")
		// SNI:sni 优先,servername 兜底(与 parseVLessNode 参数优先级一致)
		node.SNI = clashString(m, "sni")
		if node.SNI == "" {
			node.SNI = clashString(m, "servername")
		}
		node.ClientFingerprint = clashString(m, "client-fingerprint")
		node.RealityPublicKey = clashNestedString(m, "reality-opts", "public-key")
		node.RealityShortID = clashNestedString(m, "reality-opts", "short-id")
	case "trojan", "anytls":
		node.Password = clashString(m, "password")
		node.SNI = clashString(m, "sni")
		node.Insecure = clashBool(m["skip-cert-verify"])
		// trojan/anytls 协议本身基于 TLS,与行解析(parseTrojanNode/parseAnyTLSNode
		// 恒置 true)对齐,Clash 配置通常不重复声明 tls 键
		node.TLS = true
	case "ss":
		node.Cipher = clashString(m, "cipher")
		node.Password = clashString(m, "password")
		node.Plugin, node.PluginOpts = clashSSPlugin(m)
	}
	return node, nil
}

// clashSSPlugin 将 Clash ss 插件字段映射为 SIP002 形态,与 parseShadowsocksNode
// 产出对齐:plugin=obfs + plugin-opts{mode,host} -> ("simple-obfs", "obfs=<mode>;obfs-host=<host>")。
func clashSSPlugin(m map[string]any) (string, string) {
	plugin := clashString(m, "plugin")
	if plugin == "" {
		return "", ""
	}
	opts, _ := m["plugin-opts"].(map[string]any)
	if plugin == "obfs" {
		var parts []string
		if mode := clashString(opts, "mode"); mode != "" {
			parts = append(parts, "obfs="+mode)
		}
		if host := clashString(opts, "host"); host != "" {
			parts = append(parts, "obfs-host="+host)
		}
		return "simple-obfs", strings.Join(parts, ";")
	}
	// 其他插件(v2ray-plugin 等):保留插件名,opts 按 k=v 展开(排序保证确定性)
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, opts[k]))
	}
	return plugin, strings.Join(parts, ";")
}

// clashString 读取 map 字符串字段;缺失或类型不符返回空串(未知键静默忽略)。
func clashString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// clashNestedString 读取嵌套 map 的字符串字段(如 reality-opts.public-key)。
func clashNestedString(m map[string]any, group, key string) string {
	sub, _ := m[group].(map[string]any)
	if sub == nil {
		return ""
	}
	return clashString(sub, key)
}

// clashInt 容错读取数字字段:YAML 端口/alterId 可能是 int 或字符串。
func clashInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// clashBool 容错读取布尔字段(skip-cert-verify/tls 等);
// YAML 1.1 风格的 yes/on 按字符串落入 any,一并识别。
func clashBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "yes", "on":
			return true
		}
		return false
	default:
		return false
	}
}
