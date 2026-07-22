package detection

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// 直连出口(Direct Egress,CONTEXT.md 术语):检测链路的拨号出口控制。
// 本机开着 TUN 代理客户端(fake-ip 劫持 DNS+默认路由)时,节点域名经自带 DoH
// 解析为真实 IP(绕过系统 DNS),套接字绑定物理网卡(绕过 TUN 接管的默认路由),
// 保证检测结果仍是节点对宿主机的真实表现。只作用于检测链路。

// DirectEgressConfig 直连出口配置(settings 表三个键的运行时视图)
type DirectEgressConfig struct {
	Enabled   bool   // 开关;关 = 检测链路恢复系统拨号(现状行为)
	DoHURL    string // DoH 端点;host 必须是 IP 字面量(免二次解析被劫持)
	Interface string // 物理网卡名;空 = 自动识别
}

// DefaultDirectEgressConfig 默认配置:fail-open,默认开,阿里 DoH(国内可达性好,IP 字面量)。
func DefaultDirectEgressConfig() DirectEgressConfig {
	return DirectEgressConfig{
		Enabled: true,
		DoHURL:  "https://223.5.5.5/dns-query",
	}
}

// socketControl 平台相关的套接字绑定函数(net.Dialer.Control / net.ListenConfig.Control)。
// nil 表示不绑定(平台不支持或已按平台语义降级)。
type socketControl func(network, address string, c syscall.RawConn) error

// DirectDialer 直连出口拨号器,实现 mihomo 的 C.Dialer(DialContext + ListenPacket),
// 同时服务 adapter.ParseProxy 注入与 TCP 快筛替换。严格模式:DoH 解析失败或网卡
// 绑定失败 -> 明确报错,绝不静默退化为系统拨号。
type DirectDialer struct {
	iface    string
	control  socketControl
	resolver *dohResolver
}

// NewDirectDialer 装配直连拨号器。构造期完成三件可能失败的事,全部严格报错:
//  1. 网卡确定:cfg.Interface 非空用之,空则自动识别(排除虚拟接口);
//  2. 绑定能力 probe(平台语义见 bindControlFor:macOS 严格,Linux/其他降级为仅 DoH);
//  3. DoH 端点校验(host 必须 IP 字面量)。
func NewDirectDialer(cfg DirectEgressConfig, logger *slog.Logger) (*DirectDialer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	iface := cfg.Interface
	if iface == "" {
		var err error
		iface, err = detectPhysicalInterface()
		if err != nil {
			return nil, err
		}
	}

	control, err := bindControlFor(iface)
	if err != nil {
		if bindStrictPlatform {
			return nil, err
		}
		// Linux/其他平台:绑定失败(常见为非 root 缺 CAP_NET_RAW)降级为仅 DoH。
		// 此类部署通常无 TUN 劫持,DoH 已解决主要故障面;降级可见(warn 日志)。
		logger.Warn("direct egress: interface bind unavailable, continue with DoH only",
			"interface", iface, "error", err)
		control = nil
	}

	resolver, err := newDoHResolver(cfg.DoHURL, control)
	if err != nil {
		return nil, err
	}

	return &DirectDialer{iface: iface, control: control, resolver: resolver}, nil
}

// CloseIdleConnections 关闭 DoH 传输层的空闲连接(配置变更重建拨号器时调用,
// 避免旧 http.Transport 的空闲 TLS 连接占用 fd)。
func (d *DirectDialer) CloseIdleConnections() {
	if d.resolver != nil {
		d.resolver.closeIdleConnections()
	}
}

// DirectDialerCache 按配置记忆化的直连拨号器(并发安全,零值可用)。
// Detector 与 healthcheck.Checker 共用:配置相同复用同一拨号器——DoH 缓存跨节点命中、
// DoH TLS 连接经同一 http.Transport 复用、免每次检测的 net.Interfaces 系统调用;
// 配置变才重建,并关闭旧拨号器的空闲连接。构造错误同样缓存(同配置必同错,
// 避免每次检测重复探测失败路径)。
type DirectDialerCache struct {
	mu     sync.Mutex
	cfg    DirectEgressConfig
	dialer *DirectDialer
	err    error
	valid  bool // 是否已缓存过(区分零值与"缓存了 nil dialer + 错误")
}

// Get 取 cfg 对应的拨号器:命中缓存直接复用,否则装配并缓存。
func (c *DirectDialerCache) Get(cfg DirectEgressConfig) (*DirectDialer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.cfg == cfg {
		return c.dialer, c.err
	}
	if c.valid && c.dialer != nil {
		c.dialer.CloseIdleConnections()
	}
	dialer, err := NewDirectDialer(cfg, nil)
	c.cfg, c.dialer, c.err, c.valid = cfg, dialer, err, true
	return dialer, err
}

// DialContext 实现 mihomo C.Dialer:节点域名经自带 DoH 解析,套接字绑物理网卡。
// host 是 IP 字面量时跳过 DoH;回环/链路本地目标跳过网卡绑定(本机流量不经 TUN)。
func (d *DirectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("direct egress: invalid address %s: %w", address, err)
	}

	var ips []netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = d.resolver.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for _, ip := range ips {
		dialer := &net.Dialer{}
		if d.control != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			dialer.Control = d.control
		}
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	return nil, fmt.Errorf("direct egress: dial %s: %w", address, lastErr)
}

// ListenPacket 实现 mihomo C.Dialer:UDP 套接字与 TCP 一致绑定物理网卡(hysteria2 等)。
// rAddrPort 是对端地址,本地监听 socket 的绑定不需要它;对端域名解析仍由 mihomo outbound 负责。
func (d *DirectDialer) ListenPacket(ctx context.Context, network, address string, _ netip.AddrPort) (net.PacketConn, error) {
	lc := net.ListenConfig{Control: d.control}
	pc, err := lc.ListenPacket(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("direct egress: listen packet: %w", err)
	}
	return pc, nil
}

// 虚拟网卡名前缀:自动识别物理网卡时排除(TUN 客户端/回环/链路共享/桥接等)。
var virtualIfacePrefixes = []string{
	"lo", "utun", "awdl", "llw", "gif", "stf", "ap", "p2p", "anpi",
	"bridge", "vmnet", "vboxnet", "ipsec", "ppp", "tun", "tap", "wg",
	"veth", "docker", "br-", "tailscale",
}

// detectPhysicalInterface 自动识别物理网卡:按接口索引序取第一个 UP、非回环、
// 名字不含虚拟前缀、且带全局 IPv4 地址(排除链路本地 169.254/8)的接口。
// TUN 接管默认路由后,物理网卡(如 macOS en0)仍持有原 IPv4,故此策略可靠;
// 识别失败时用户可在设置 direct_egress_interface 显式指定。
func detectPhysicalInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("direct egress: list interfaces: %w", err)
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualIface(ifi.Name) {
			continue
		}
		if hasGlobalIPv4(&ifi) {
			return ifi.Name, nil
		}
	}
	return "", fmt.Errorf("direct egress: 自动识别物理网卡失败:没有 UP 且带全局 IPv4 地址的非虚拟接口(已排除 utun/lo/awdl/llw 等);请在设置 direct_egress_interface 中显式指定")
}

func isVirtualIface(name string) bool {
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func hasGlobalIPv4(ifi *net.Interface) bool {
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil && !ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// DoH 解析缓存 TTL 的钳制区间(尊重上游 TTL 但防过短抖动/过长陈旧)。
const (
	dohCacheMinTTL = 15 * time.Second
	dohCacheMaxTTL = 5 * time.Minute
)

// dohResolver 自带 DoH 解析器:绕过系统 resolver(fake-ip 劫持面),
// 向 IP 字面量端点发 RFC 8484 wireformat 查询(application/dns-message,
// 阿里 223.5.5.5 与 Cloudflare 1.1.1.1 的 /dns-query 均只认此格式)。结果短缓存。
// DoH 请求自身也绑物理网卡(端点是 IP 字面量,无鸡生蛋解析问题)。
type dohResolver struct {
	baseURL string
	client  *http.Client

	mu    sync.Mutex
	cache map[string]dohCacheEntry

	// group 合并同名并发查询:拨号器按配置记忆化共享后,多节点同时解析同一
	// 域名只发一次 DoH 请求,其余等结果共享(含错误)。
	group singleflight.Group
}

type dohCacheEntry struct {
	ips     []netip.Addr
	expires time.Time
}

// newDoHResolver 构造 DoH 解析器。端点 host 必须是 IP 字面量:
// 域名 host 需要二次解析,而系统 resolver 正是要绕开的劫持面,构造期拒绝。
func newDoHResolver(rawURL string, control socketControl) (*dohResolver, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("direct egress: invalid DoH URL %q", rawURL)
	}
	host := u.Hostname()
	if _, err := netip.ParseAddr(host); err != nil {
		return nil, fmt.Errorf("direct egress: DoH endpoint host must be an IP literal (got %q): 域名 host 需二次解析,会落入被劫持的系统 DNS", host)
	}

	transport := &http.Transport{
		Proxy:             nil, // 经环境变量代理会重新落入 TUN 劫持面
		DialContext:       (&net.Dialer{Timeout: 5 * time.Second, Control: control}).DialContext,
		ForceAttemptHTTP2: true,
	}
	return &dohResolver{
		baseURL: rawURL,
		client:  &http.Client{Transport: transport, Timeout: 5 * time.Second},
		cache:   make(map[string]dohCacheEntry),
	}, nil
}

// closeIdleConnections 关闭 DoH 传输层空闲连接(见 DirectDialer.CloseIdleConnections)。
func (r *dohResolver) closeIdleConnections() {
	if t, ok := r.client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

// resolve 经 DoH 把 host 解析为 IP 列表(优先 A,无 A 记录退 AAAA)。严格模式:
// 任何失败(传输/状态码/NXDOMAIN/无记录)都报错,调用方绝不回落系统 resolver。
// 同名并发查询经 singleflight 合并为一次 DoH 请求。
func (r *dohResolver) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	if ent, ok := r.cache[host]; ok && time.Now().Before(ent.expires) {
		ips := ent.ips
		r.mu.Unlock()
		return ips, nil
	}
	r.mu.Unlock()

	v, err, _ := r.group.Do(host, func() (any, error) {
		return r.resolveUncached(ctx, host)
	})
	if err != nil {
		return nil, err
	}
	return v.([]netip.Addr), nil
}

// resolveUncached 缓存未命中时的真实解析(singleflight 内单实例执行)。
func (r *dohResolver) resolveUncached(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, ttl, err := r.query(ctx, host, dns.TypeA)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		if ips, ttl, err = r.query(ctx, host, dns.TypeAAAA); err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("direct egress: doh: no A/AAAA records for %s", host)
	}

	if ttl < dohCacheMinTTL {
		ttl = dohCacheMinTTL
	}
	if ttl > dohCacheMaxTTL {
		ttl = dohCacheMaxTTL
	}
	r.mu.Lock()
	// 惰性过期淘汰:写入时顺手清掉已过期条目,缓存 map 不随节点域名数无限增长。
	now := time.Now()
	for k, ent := range r.cache {
		if !now.Before(ent.expires) {
			delete(r.cache, k)
		}
	}
	r.cache[host] = dohCacheEntry{ips: ips, expires: now.Add(ttl)}
	r.mu.Unlock()
	return ips, nil
}

// query 发一次 RFC 8484 wireformat DoH 查询(POST application/dns-message),
// 返回指定类型的地址与应答最小 TTL。
func (r *dohResolver) query(ctx context.Context, host string, qtype uint16) ([]netip.Addr, time.Duration, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(host), qtype)
	msg.RecursionDesired = true
	packed, err := msg.Pack()
	if err != nil {
		return nil, 0, fmt.Errorf("direct egress: doh: pack query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL, bytes.NewReader(packed))
	if err != nil {
		return nil, 0, fmt.Errorf("direct egress: doh: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("direct egress: doh query %s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("direct egress: doh query %s: http status %d", host, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, 0, fmt.Errorf("direct egress: doh query %s: read body: %w", host, err)
	}
	answer := new(dns.Msg)
	if err := answer.Unpack(body); err != nil {
		return nil, 0, fmt.Errorf("direct egress: doh query %s: unpack: %w", host, err)
	}
	if answer.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("direct egress: doh query %s: rcode %s", host, dns.RcodeToString[answer.Rcode])
	}

	var ips []netip.Addr
	minTTL := dohCacheMaxTTL
	for _, rr := range answer.Answer {
		var ip netip.Addr
		switch a := rr.(type) {
		case *dns.A:
			if qtype != dns.TypeA {
				continue
			}
			if v, ok := netip.AddrFromSlice(a.A); ok {
				ip = v
			}
		case *dns.AAAA:
			if qtype != dns.TypeAAAA {
				continue
			}
			if v, ok := netip.AddrFromSlice(a.AAAA); ok {
				ip = v
			}
		default:
			continue
		}
		if !ip.IsValid() {
			continue
		}
		ips = append(ips, ip)
		if d := time.Duration(rr.Header().Ttl) * time.Second; d < minTTL {
			minTTL = d
		}
	}
	return ips, minTTL, nil
}
