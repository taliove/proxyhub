package detection

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
	"github.com/taliove/proxyhub/internal/generator"
	"github.com/taliove/proxyhub/internal/subscription"
)

// ProxyAdapter mihomo 代理适配器(对外隐藏 mihomo 细节)
type ProxyAdapter struct {
	proxy C.ProxyAdapter
}

// NewProxyAdapter 从 Node 构造 mihomo adapter
func NewProxyAdapter(node *subscription.Node) (*ProxyAdapter, error) {
	// 复用 generator.ClashProxy 把 Node 转成 Clash 配置 map
	proxyMap, err := generator.ClashProxy(node, node.Name)
	if err != nil {
		return nil, fmt.Errorf("convert node to clash config: %w", err)
	}

	// mihomo 的 adapter.ParseProxy 从配置 map 构造 ProxyAdapter
	proxy, err := adapter.ParseProxy(proxyMap)
	if err != nil {
		return nil, fmt.Errorf("parse mihomo proxy: %w", err)
	}

	return &ProxyAdapter{proxy: proxy}, nil
}

// DialContext 通过代理建立连接
func (p *ProxyAdapter) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// 解析 address (host:port)
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", address, err)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid port %s: %w", portStr, err)
	}

	metadata := &C.Metadata{
		NetWork: C.TCP,
		Host:    host,
		DstPort: uint16(port),
	}

	// 通过 mihomo adapter 建立代理连接
	conn, err := p.proxy.DialContext(ctx, metadata)
	if err != nil {
		return nil, fmt.Errorf("dial via proxy: %w", err)
	}

	return conn, nil
}

// requestViaProxy 通过代理发 HTTP 请求
func (d *Detector) requestViaProxy(ctx context.Context, proxyAdapter *ProxyAdapter, target Target) (*http.Response, error) {
	// 构造自定义 Transport,使用 proxyAdapter 的 DialContext
	transport := &http.Transport{
		DialContext: proxyAdapter.DialContext,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   d.requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// 添加自定义 headers
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	}

	return client.Do(req)
}

// checkUnlockStatus 检查响应是否符合解锁条件(状态码 + 关键字)
func checkUnlockStatus(resp *http.Response, target Target) (bool, string) {
	// 检查状态码
	statusOK := false
	for _, expect := range target.ExpectStatus {
		if resp.StatusCode == expect {
			statusOK = true
			break
		}
	}
	if !statusOK {
		return false, fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}

	// 读取响应体
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return false, fmt.Sprintf("read body: %v", err)
	}
	bodyStr := string(body)

	// 检查排除关键字
	for _, exclude := range target.ResponseExcludes {
		if strings.Contains(bodyStr, exclude) {
			return false, fmt.Sprintf("blocked: found '%s'", exclude)
		}
	}

	// 检查包含关键字
	if len(target.ResponseContains) > 0 {
		for _, contain := range target.ResponseContains {
			if !strings.Contains(bodyStr, contain) {
				return false, fmt.Sprintf("missing keyword '%s'", contain)
			}
		}
	}

	return true, ""
}
