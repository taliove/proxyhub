package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// Result 健康检查结果
type Result struct {
	Node      *subscription.Node
	Available bool
	Latency   int // 毫秒
	Error     error
}

// Checker 健康检查器
type Checker struct {
	latencyTimeout time.Duration
	requestTimeout time.Duration
	testURL        string
	concurrent     int

	// directEgressConfig 返回直连出口配置(nil = 不启用,保持系统拨号)。
	// 用函数注入:每次检查实时读取(settings 热改无需重启),且线程安全。
	// 与 detection.Detector 的 provider 模式一致(ticket 0019/0020)。
	directEgressConfig func() detection.DirectEgressConfig

	// directDialerCache 按配置记忆化直连拨号器(配置相同复用,变才重建;
	// 并发安全,与 detection.Detector 共用同一实现)。
	directDialerCache detection.DirectDialerCache
}

// NewChecker 创建健康检查器
func NewChecker(latencyTimeout, requestTimeout time.Duration, testURL string, concurrent int) *Checker {
	return &Checker{
		latencyTimeout: latencyTimeout,
		requestTimeout: requestTimeout,
		testURL:        testURL,
		concurrent:     concurrent,
	}
}

// SetDirectEgressConfigProvider 注入直连出口配置提供函数(每次检查调用,返回实时配置)。
// 未注入或开关关 = 健康检查保持系统拨号(现状行为)。
func (c *Checker) SetDirectEgressConfigProvider(provider func() detection.DirectEgressConfig) {
	c.directEgressConfig = provider
}

// directDialer 按当前设置取直连拨号器;未注入 provider 或开关关返回 (nil, nil)。
// 拨号器按配置记忆化(见 detection.DirectDialerCache);装配失败(网卡识别/绑定/
// DoH 端点校验)严格报错,绝不静默退化为系统拨号(0019 严格模式)。
func (c *Checker) directDialer() (*detection.DirectDialer, error) {
	if c.directEgressConfig == nil {
		return nil, nil
	}
	cfg := c.directEgressConfig()
	if !cfg.Enabled {
		return nil, nil
	}
	return c.directDialerCache.Get(cfg)
}

// dialTCP 建立一次 TCP 连接并立即关闭(连通性探测)。
// 直连出口开启时经直连拨号器(DoH 解析 + 绑物理网卡,TUN 共存下不对假 IP 假通);
// 否则用系统 dialer(现状行为)。拨号器装配/拨号错误原样上抛。
func (c *Checker) dialTCP(ctx context.Context, addr string) error {
	direct, err := c.directDialer()
	if err != nil {
		return err // 严格:装配失败直接报错,不静默退化
	}
	if direct != nil {
		conn, err := direct.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// CheckAll 并发检查所有节点
func (c *Checker) CheckAll(ctx context.Context, nodes []*subscription.Node) []*Result {
	sem := make(chan struct{}, c.concurrent)
	results := make([]*Result, len(nodes))
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n *subscription.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = c.Check(ctx, n)
		}(i, node)
	}

	wg.Wait()
	return results
}

// Check 检查单个节点（延迟测速 + 真实请求测试）
func (c *Checker) Check(ctx context.Context, node *subscription.Node) *Result {
	result := &Result{Node: node, Available: false}

	// 阶段 1：延迟测速
	latency, err := c.measureLatency(ctx, node)
	if err != nil {
		result.Error = fmt.Errorf("latency check: %w", err)
		return result
	}
	result.Latency = latency

	// 阶段 2：真实请求测试（通过代理访问测试 URL）
	if err := c.testRealRequest(ctx, node); err != nil {
		result.Error = fmt.Errorf("real request: %w", err)
		return result
	}

	result.Available = true
	return result
}

// measureLatency 测量到节点的延迟（TCP 连接时间）
func (c *Checker) measureLatency(ctx context.Context, node *subscription.Node) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.latencyTimeout)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", node.Server, node.Port)
	start := time.Now()

	if err := c.dialTCP(ctx, addr); err != nil {
		return 0, err
	}

	elapsed := time.Since(start)
	latency := int(elapsed.Milliseconds())
	if latency == 0 {
		// 亚毫秒级延迟，向上取整为 1ms
		latency = 1
	}
	return latency, nil
}

// testRealRequest 通过节点访问测试 URL（简化版，不建立完整代理连接）
// 实际生产环境应该用完整的代理客户端库（如 mihomo），这里为简化演示，只测 TCP 连通性
func (c *Checker) testRealRequest(ctx context.Context, node *subscription.Node) error {
	// TODO: 实际应该建立代理连接并通过它发起 HTTP 请求
	// 目前简化为：如果 TCP 连接成功，假设节点可用
	// 完整实现需要集成 mihomo 或类似库

	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", node.Server, node.Port)
	return c.dialTCP(ctx, addr)
}

// testRealRequestViaHTTP 通过 HTTP 代理测试（仅支持 HTTP/SOCKS5 代理）
func (c *Checker) testRealRequestViaHTTP(ctx context.Context, proxyURL string) error {
	client := &http.Client{
		Timeout: c.requestTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.testURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	// 读取少量内容确保连接完整
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return nil
}
