package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// subscriptionUserAgent 订阅拉取 UA。
// 必须用 v2rayN 系 UA:机场按 UA 决定返回格式,Clash 系 UA 会返回 YAML(本解析器不支持),
// 而 Go 默认 UA(Go-http-client)常被机场直接拒绝(401/403)。
const subscriptionUserAgent = "v2rayN/6.23"

// Fetcher 订阅获取器
type Fetcher struct {
	client *http.Client
}

// NewFetcher 创建订阅获取器
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// FetchDiagnostics 单次订阅拉取的结构化诊断(ticket 0018)。
// 口径与机场测试 RunDiagnostic 对齐:HTTP 状态、拉取耗时、解析成功节点数、解析失败行数。
type FetchDiagnostics struct {
	HTTPStatus    int   `json:"http_status"`    // 0 = 请求未发出/网络错误
	DurationMs    int64 `json:"duration_ms"`    // 请求发出到 body 读完
	NodeCount     int   `json:"node_count"`     // 解析成功节点数
	ParseFailures int   `json:"parse_failures"` // 解析失败行数(非空行中无法解析的)
}

// Fetch 从 URL 获取订阅
func (f *Fetcher) Fetch(name, subscriptionURL string) (*Subscription, error) {
	sub, _, err := f.FetchWithDiagnostics(name, subscriptionURL)
	return sub, err
}

// FetchWithDiagnostics 从 URL 获取订阅,并返回结构化拉取诊断。
// diag 在请求已尝试时恒非 nil(含错误路径):网络错误 HTTPStatus=0,
// 非 200 响应带真实状态码;调用方无论成败都可落诊断。
func (f *Fetcher) FetchWithDiagnostics(name, subscriptionURL string) (*Subscription, *FetchDiagnostics, error) {
	diag := &FetchDiagnostics{}
	start := time.Now()

	req, err := http.NewRequest(http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return nil, diag, fmt.Errorf("build subscription request: %w", err)
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		diag.DurationMs = time.Since(start).Milliseconds()
		return nil, diag, fmt.Errorf("fetch subscription: %w", err)
	}
	defer resp.Body.Close()
	diag.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		diag.DurationMs = time.Since(start).Milliseconds()
		return nil, diag, fmt.Errorf("fetch subscription: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	diag.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return nil, diag, fmt.Errorf("read subscription body: %w", err)
	}

	// 尝试 Base64 解码（订阅整体可能是 base64 编码，支持 raw/padded）
	decoded, err := base64.RawStdEncoding.DecodeString(string(body))
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			// 如果不是 Base64，直接使用原始内容
			decoded = body
		}
	}

	// 复用 ParseWithStats 的解析统计口径(与机场测试诊断同源)
	parsed := ParseWithStats(string(decoded), name)
	diag.NodeCount = len(parsed.Nodes)
	diag.ParseFailures = parsed.ParseFailures
	if len(parsed.Nodes) == 0 {
		return nil, diag, fmt.Errorf("parse subscription: no valid nodes found")
	}

	return &Subscription{
		Name:  name,
		URL:   subscriptionURL,
		Nodes: parsed.Nodes,
	}, diag, nil
}

// parse 解析订阅内容
func (f *Fetcher) parse(content, source string) ([]*Node, error) {
	lines := strings.Split(content, "\n")
	var nodes []*Node

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		node, err := f.parseNode(line, source)
		if err != nil {
			// 跳过无法解析的节点，继续处理其他节点
			continue
		}

		// 跳过套餐信息伪节点：部分机场把「剩余流量/套餐到期/距离下次重置」
		// 也编码成合法的 anytls/vmess 链接（复用真实 server:port），解析后会变成
		// 假节点污染节点池。按名称模式识别并丢弃（见 isMetadataName）。
		if isMetadataName(node.Name) {
			continue
		}

		// 保留原始分享 URI,供 share-uri 端点原样回放节点二维码(见 ticket 56)。
		// line 已是 TrimSpace 后的完整原始链接;含凭证,只落到 Node.RawLink(json:"-")。
		node.RawLink = line

		nodes = append(nodes, node)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no valid nodes found")
	}

	return nodes, nil
}

// parseNode 解析单个节点
func (f *Fetcher) parseNode(line, source string) (*Node, error) {
	// 支持的协议：vmess://, vless://, trojan://, ss://
	if strings.HasPrefix(line, "vmess://") {
		return f.parseVMessNode(line, source)
	} else if strings.HasPrefix(line, "vless://") {
		return f.parseVLessNode(line, source)
	} else if strings.HasPrefix(line, "trojan://") {
		return f.parseTrojanNode(line, source)
	} else if strings.HasPrefix(line, "ss://") {
		return f.parseShadowsocksNode(line, source)
	} else if strings.HasPrefix(line, "anytls://") {
		return f.parseAnyTLSNode(line, source)
	}

	return nil, fmt.Errorf("unsupported protocol")
}

// parseVMessNode 解析 VMess 节点
func (f *Fetcher) parseVMessNode(line, source string) (*Node, error) {
	// vmess:// 后面是 Base64 编码的 JSON
	encoded := strings.TrimPrefix(line, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// 尝试 URL-safe Base64
		decoded, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode vmess: %w", err)
		}
	}

	var vmess struct {
		Add  string `json:"add"`
		Port any    `json:"port"` // 可能是字符串或数字
		ID   string `json:"id"`
		Aid  any    `json:"aid"` // 可能是字符串或数字
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		PS   string `json:"ps"` // 节点名称
	}

	if err := json.Unmarshal(decoded, &vmess); err != nil {
		return nil, fmt.Errorf("parse vmess json: %w", err)
	}

	port := f.parsePort(vmess.Port)
	alterID := f.parseInt(vmess.Aid)

	region := extractRegion(vmess.PS)

	return &Node{
		Name:      vmess.PS,
		Type:      "vmess",
		Server:    vmess.Add,
		Port:      port,
		UUID:      vmess.ID,
		AlterID:   alterID,
		Network:   vmess.Net,
		TLS:       vmess.TLS == "tls",
		Region:    region,
		Source:    source,
		Available: false,
	}, nil
}

// parseVLessNode 解析 VLess 节点
func (f *Fetcher) parseVLessNode(line, source string) (*Node, error) {
	// vless://[uuid]@[server]:[port]?[params]#[name]
	line = strings.TrimPrefix(line, "vless://")

	parts := strings.SplitN(line, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid vless format")
	}

	uuid := parts[0]
	rest := parts[1]

	// 分离 server:port 和参数
	var serverPort, params, name string
	if idx := strings.Index(rest, "?"); idx != -1 {
		serverPort = rest[:idx]
		rest = rest[idx+1:]
		if idx := strings.Index(rest, "#"); idx != -1 {
			params = rest[:idx]
			name, _ = url.QueryUnescape(rest[idx+1:])
		} else {
			params = rest
		}
	} else if idx := strings.Index(rest, "#"); idx != -1 {
		serverPort = rest[:idx]
		name, _ = url.QueryUnescape(rest[idx+1:])
	} else {
		serverPort = rest
	}

	serverParts := strings.Split(serverPort, ":")
	if len(serverParts) != 2 {
		return nil, fmt.Errorf("invalid server:port format")
	}

	server := serverParts[0]
	port, err := strconv.Atoi(serverParts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	// 解析参数
	query, _ := url.ParseQuery(params)
	network := query.Get("type")
	if network == "" {
		network = "tcp"
	}
	security := query.Get("security")

	region := extractRegion(name)

	return &Node{
		Name:      name,
		Type:      "vless",
		Server:    server,
		Port:      port,
		UUID:      uuid,
		Network:   network,
		TLS:       security == "tls",
		Region:    region,
		Source:    source,
		Available: false,
	}, nil
}

// parseTrojanNode 解析 Trojan 节点
func (f *Fetcher) parseTrojanNode(line, source string) (*Node, error) {
	// trojan://[password]@[server]:[port]?[params]#[name]
	line = strings.TrimPrefix(line, "trojan://")

	parts := strings.SplitN(line, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid trojan format")
	}

	password := parts[0]
	rest := parts[1]

	var serverPort, name string
	rest2 := rest
	if idx := strings.Index(rest2, "#"); idx != -1 {
		name, _ = url.QueryUnescape(rest2[idx+1:])
		rest2 = rest2[:idx]
	}
	if idx := strings.Index(rest2, "?"); idx != -1 {
		rest2 = rest2[:idx]
	}
	serverPort = rest2

	serverParts := strings.Split(serverPort, ":")
	if len(serverParts) != 2 {
		return nil, fmt.Errorf("invalid server:port format")
	}

	server := serverParts[0]
	port, err := strconv.Atoi(serverParts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	region := extractRegion(name)

	return &Node{
		Name:      name,
		Type:      "trojan",
		Server:    server,
		Port:      port,
		Password:  password,
		TLS:       true, // Trojan 默认使用 TLS
		Region:    region,
		Source:    source,
		Available: false,
	}, nil
}

// parseAnyTLSNode 解析 AnyTLS 节点
// 格式：anytls://[password]@[server]:[port]?sni=xxx&insecure=1#[name]
// 结构与 Trojan 类似，但携带 sni / insecure 参数，且 password 常为 UUID 形式。
func (f *Fetcher) parseAnyTLSNode(line, source string) (*Node, error) {
	line = strings.TrimPrefix(line, "anytls://")

	parts := strings.SplitN(line, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid anytls format")
	}

	password := parts[0]
	rest := parts[1]

	// 拆出 #name
	var name string
	if idx := strings.Index(rest, "#"); idx != -1 {
		name, _ = url.QueryUnescape(rest[idx+1:])
		rest = rest[:idx]
	}

	// 拆出 ?params
	var params string
	if idx := strings.Index(rest, "?"); idx != -1 {
		params = rest[idx+1:]
		rest = rest[:idx]
	}

	serverParts := strings.Split(rest, ":")
	if len(serverParts) != 2 {
		return nil, fmt.Errorf("invalid server:port format")
	}
	server := serverParts[0]
	port, err := strconv.Atoi(serverParts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	query, _ := url.ParseQuery(params)
	sni := query.Get("sni")
	insecure := query.Get("insecure") == "1" || query.Get("insecure") == "true"

	region := extractRegion(name)

	return &Node{
		Name:      name,
		Type:      "anytls",
		Server:    server,
		Port:      port,
		Password:  password,
		SNI:       sni,
		Insecure:  insecure,
		TLS:       true, // AnyTLS 始终基于 TLS
		Region:    region,
		Source:    source,
		Available: false,
	}, nil
}

// metadataKeywords 是套餐信息伪节点名称里的特征词。命中任一即视为非真实节点。
// 这些"节点"由机场把流量/到期信息编码成分享链接产生（复用真实 server:port），
// 不含出口能力，须在解析层丢弃，否则会占用节点位甚至因去重挤掉真实节点。
var metadataKeywords = []string{
	"剩余流量", "套餐到期", "距离下次", "重置", "到期", "过期",
	"官网", "官方", "订阅", "群组", "客服", "网址", "剩余",
}

// isMetadataName 报告节点名是否为套餐信息伪节点（子串匹配任一特征词）。
func isMetadataName(name string) bool {
	for _, kw := range metadataKeywords {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

// parseShadowsocksNode 解析 Shadowsocks 节点
// 支持两种格式：
// 1. 老格式: ss://base64(method:password@server:port)#name
// 2. SIP002: ss://base64(method:password)@server:port/?plugin=xxx#name
func (f *Fetcher) parseShadowsocksNode(line, source string) (*Node, error) {
	line = strings.TrimPrefix(line, "ss://")

	// 提取 #name
	var name string
	if idx := strings.Index(line, "#"); idx != -1 {
		name, _ = url.QueryUnescape(line[idx+1:])
		line = line[:idx]
	}

	// 提取 SIP002 plugin 参数(如 ?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dx)。
	// 必须在剥离查询串之前解析:obfs/v2ray-plugin 信息丢失会让重建的订阅不可用
	// (服务器只认插件包装后的流量,裸 SS 握手直接失败)。
	var plugin, pluginOpts string
	if idx := strings.Index(line, "?"); idx != -1 {
		plugin, pluginOpts = parseSSPlugin(line[idx+1:])
		line = line[:idx]
	}
	line = strings.TrimSuffix(line, "/")

	var method, password, server string
	var port int

	// 判断格式：有 @ 在外层 → SIP002；无 @ → 老格式需整体解码
	if strings.Contains(line, "@") {
		// SIP002: base64(method:password)@server:port
		parts := strings.SplitN(line, "@", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid SIP002 format")
		}

		// 解码 userinfo 部分（尝试多种 base64 编码：raw/padded, url/std）
		userInfo := parts[0]
		decoded, err := base64.RawURLEncoding.DecodeString(userInfo)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(userInfo)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(userInfo)
				if err != nil {
					decoded, err = base64.StdEncoding.DecodeString(userInfo)
					if err != nil {
						return nil, fmt.Errorf("decode userinfo: %w", err)
					}
				}
			}
		}

		methodPassword := strings.SplitN(string(decoded), ":", 2)
		if len(methodPassword) != 2 {
			return nil, fmt.Errorf("invalid method:password in userinfo")
		}
		method = methodPassword[0]
		password = methodPassword[1]

		// 解析 server:port
		serverPort := strings.Split(parts[1], ":")
		if len(serverPort) != 2 {
			return nil, fmt.Errorf("invalid server:port format")
		}
		server = serverPort[0]
		var err2 error
		port, err2 = strconv.Atoi(serverPort[1])
		if err2 != nil {
			return nil, fmt.Errorf("invalid port: %w", err2)
		}
	} else {
		// 老格式: base64(method:password@server:port)
		decoded, err := base64.RawURLEncoding.DecodeString(line)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(line)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(line)
				if err != nil {
					decoded, err = base64.StdEncoding.DecodeString(line)
					if err != nil {
						return nil, fmt.Errorf("decode legacy ss: %w", err)
					}
				}
			}
		}

		content := string(decoded)
		parts := strings.SplitN(content, "@", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid legacy ss format")
		}

		methodPassword := strings.SplitN(parts[0], ":", 2)
		if len(methodPassword) != 2 {
			return nil, fmt.Errorf("invalid method:password format")
		}
		method = methodPassword[0]
		password = methodPassword[1]

		serverPort := strings.Split(parts[1], ":")
		if len(serverPort) != 2 {
			return nil, fmt.Errorf("invalid server:port format")
		}
		server = serverPort[0]
		var err2 error
		port, err2 = strconv.Atoi(serverPort[1])
		if err2 != nil {
			return nil, fmt.Errorf("invalid port: %w", err2)
		}
	}

	region := extractRegion(name)

	return &Node{
		Name:       name,
		Type:       "ss",
		Server:     server,
		Port:       port,
		Password:   password,
		Cipher:     method,
		Plugin:     plugin,
		PluginOpts: pluginOpts,
		Region:     region,
		Source:     source,
		Available:  false,
	}, nil
}

// parseSSPlugin 解析 SIP002 查询串中的 plugin 参数,返回 (插件名, opts 串)。
// 形如 "plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dx" -> ("simple-obfs", "obfs=http;obfs-host=x")
// 不用 url.ParseQuery:它对未转义的字面分号报错(Go 1.17+),而部分客户端/机场
// 直接输出 "plugin=simple-obfs;obfs=http;obfs-host=x",两种形态都要兼容。
func parseSSPlugin(query string) (string, string) {
	var raw string
	for _, seg := range strings.Split(query, "&") {
		if v, ok := strings.CutPrefix(seg, "plugin="); ok {
			raw = v
			break
		}
	}
	if raw == "" {
		return "", ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return "", ""
	}
	name, opts, _ := strings.Cut(decoded, ";")
	return name, opts
}

// extractRegion 从节点名称提取地区
// extractRegion 从节点名称提取地区代码。支持中文名、英文名、emoji 国旗、ISO 代码。
// 匹配优先级:台湾/香港等敏感词优先(避免被 🇨🇳 误盖),再 emoji,再一般中英文名,最后 ISO 代码。
func extractRegion(name string) string {
	nameUpper := strings.ToUpper(name)

	// 优先级0: 台湾/香港/澳门等显式文字(避免被 🇨🇳 emoji 误覆盖)
	priorityRegions := map[string]string{
		"台湾": "TW", "TAIWAN": "TW",
		"香港": "HK", "HONG KONG": "HK", "HONGKONG": "HK",
		"澳门": "MO", "MACAO": "MO", "MACAU": "MO",
	}
	for key, code := range priorityRegions {
		if strings.Contains(nameUpper, key) {
			return code
		}
	}

	// 优先级1: emoji 国旗(Unicode 区域指示符,两个字符组成一面旗)
	// 🇭🇰 = U+1F1ED U+1F1F0 (Regional Indicator H + K)
	emojiFlags := map[string]string{
		"🇭🇰": "HK", "🇲🇴": "MO", "🇹🇼": "TW", "🇨🇳": "CN",
		"🇯🇵": "JP", "🇰🇷": "KR", "🇸🇬": "SG", "🇹🇭": "TH",
		"🇻🇳": "VN", "🇲🇾": "MY", "🇵🇭": "PH", "🇮🇩": "ID",
		"🇮🇳": "IN", "🇵🇰": "PK", "🇧🇩": "BD", "🇱🇰": "LK",
		"🇲🇲": "MM", "🇰🇭": "KH", "🇱🇦": "LA", "🇦🇪": "AE",
		"🇸🇦": "SA", "🇮🇷": "IR", "🇮🇶": "IQ", "🇮🇱": "IL",
		"🇹🇷": "TR", "🇰🇿": "KZ", "🇺🇿": "UZ", "🇦🇲": "AM",
		"🇺🇸": "US", "🇨🇦": "CA", "🇲🇽": "MX", "🇧🇷": "BR",
		"🇦🇷": "AR", "🇨🇱": "CL", "🇨🇴": "CO", "🇵🇪": "PE",
		"🇬🇧": "GB", "🇩🇪": "DE", "🇫🇷": "FR", "🇮🇹": "IT",
		"🇪🇸": "ES", "🇳🇱": "NL", "🇨🇭": "CH", "🇸🇪": "SE",
		"🇳🇴": "NO", "🇩🇰": "DK", "🇫🇮": "FI", "🇵🇱": "PL",
		"🇷🇺": "RU", "🇺🇦": "UA", "🇬🇷": "GR", "🇵🇹": "PT",
		"🇦🇺": "AU", "🇳🇿": "NZ", "🇿🇦": "ZA", "🇪🇬": "EG",
		"🇳🇬": "NG", "🇰🇪": "KE", "🇪🇹": "ET", "🇲🇦": "MA",
	}
	for emoji, code := range emojiFlags {
		if strings.Contains(name, emoji) {
			return code
		}
	}

	// 优先级2: 其他中英文全名(子串匹配,不区分大小写)
	regionNames := map[string]string{
		"日本": "JP", "JAPAN": "JP",
		"韩国": "KR", "KOREA": "KR", "首尔": "KR", "SEOUL": "KR",
		"新加坡": "SG", "SINGAPORE": "SG",
		"泰国": "TH", "THAILAND": "TH", "曼谷": "TH",
		"越南": "VN", "VIETNAM": "VN",
		"马来西亚": "MY", "MALAYSIA": "MY",
		"菲律宾": "PH", "PHILIPPINES": "PH",
		"印度尼西亚": "ID", "INDONESIA": "ID",
		"印度": "IN", "INDIA": "IN",
		"巴基斯坦": "PK", "PAKISTAN": "PK",
		"孟加拉": "BD", "BANGLADESH": "BD",
		"缅甸": "MM", "MYANMAR": "MM",
		"柬埔寨": "KH", "CAMBODIA": "KH",
		"迪拜": "AE", "DUBAI": "AE",
		"沙特": "SA", "SAUDI": "SA",
		"伊拉克": "IQ", "IRAQ": "IQ",
		"以色列": "IL", "ISRAEL": "IL",
		"土耳其": "TR", "TURKEY": "TR",
		"哈萨克斯坦": "KZ", "KAZAKHSTAN": "KZ",
		"美国": "US", "UNITED STATES": "US", "USA": "US", "AMERICA": "US",
		"加拿大": "CA", "CANADA": "CA",
		"墨西哥": "MX", "MEXICO": "MX",
		"巴西": "BR", "BRAZIL": "BR",
		"阿根廷": "AR", "ARGENTINA": "AR", "ARGENTINE": "AR",
		"智利": "CL", "CHILE": "CL",
		"哥伦比亚": "CO", "COLOMBIA": "CO",
		"英国": "GB", "UNITED KINGDOM": "GB", "UK": "GB", "BRITAIN": "GB",
		"德国": "DE", "GERMANY": "DE",
		"法国": "FR", "FRANCE": "FR",
		"意大利": "IT", "ITALY": "IT",
		"西班牙": "ES", "SPAIN": "ES",
		"荷兰": "NL", "NETHERLANDS": "NL",
		"瑞士": "CH", "SWITZERLAND": "CH",
		"瑞典": "SE", "SWEDEN": "SE",
		"挪威": "NO", "NORWAY": "NO",
		"丹麦": "DK", "DENMARK": "DK",
		"芬兰": "FI", "FINLAND": "FI",
		"波兰": "PL", "POLAND": "PL",
		"俄罗斯": "RU", "RUSSIA": "RU", "莫斯科": "RU", "MOSCOW": "RU",
		"乌克兰": "UA", "UKRAINE": "UA",
		"希腊": "GR", "GREECE": "GR",
		"葡萄牙": "PT", "PORTUGAL": "PT",
		"澳大利亚": "AU", "AUSTRALIA": "AU", "悉尼": "AU", "SYDNEY": "AU",
		"新西兰": "NZ", "NEW ZEALAND": "NZ",
		"南非": "ZA", "SOUTH AFRICA": "ZA",
		"埃及": "EG", "EGYPT": "EG",
		"尼日利亚": "NG", "NIGERIA": "NG",
	}
	for key, code := range regionNames {
		if strings.Contains(nameUpper, key) {
			return code
		}
	}

	// 优先级3: ISO 两字母代码(最后匹配,避免误命中 HKBN、USDT 之类)
	isoCodes := []string{
		"HK", "MO", "TW", "CN", "JP", "KR", "SG", "TH", "VN", "MY",
		"PH", "ID", "IN", "PK", "BD", "MM", "KH", "AE", "SA", "IQ",
		"IL", "TR", "KZ", "US", "CA", "MX", "BR", "AR", "CL", "CO",
		"GB", "DE", "FR", "IT", "ES", "NL", "CH", "SE", "NO", "DK",
		"FI", "PL", "RU", "UA", "GR", "PT", "AU", "NZ", "ZA", "EG", "NG",
	}
	for _, code := range isoCodes {
		if strings.Contains(nameUpper, code) {
			return code
		}
	}

	return "Unknown"
}

// parsePort 解析端口（支持字符串和数字）
func (f *Fetcher) parsePort(port any) int {
	switch v := port.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		p, _ := strconv.Atoi(v)
		return p
	default:
		return 0
	}
}

// parseInt 解析整数（支持字符串和数字）
func (f *Fetcher) parseInt(val any) int {
	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}
