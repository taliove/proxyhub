package detection

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// loopbackIface 返回本机回环网卡名(显式指定网卡的测试用;自动识别不覆盖回环)。
func loopbackIface() string {
	if runtime.GOOS == "darwin" {
		return "lo0"
	}
	return "lo"
}

// dohFixture 起一个 mock DoH 服务(RFC 8484 wireformat),把 example.com 解析为 127.0.0.1。
// 返回 DoH URL(IP 字面量 host,满足拨号器的字面量约束)。
func dohFixture(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read doh query: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			q := new(dns.Msg)
			if err := q.Unpack(body); err != nil {
				t.Errorf("unpack doh query: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if got := q.Question[0].Name; got != "example.com." {
				t.Errorf("doh query name = %q, want example.com.", got)
			}
			resp := new(dns.Msg)
			resp.SetReply(q)
			if q.Question[0].Qtype == dns.TypeA {
				resp.Answer = []dns.RR{&dns.A{
					Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("127.0.0.1").To4(),
				}}
			}
			packed, err := resp.Pack()
			if err != nil {
				t.Errorf("pack doh response: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/dns-message")
			w.Write(packed)
		}
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// tcpEchoListener 起一个本地 TCP 服务,接受即关,返回 host:port 中的 port。
func tcpEchoListener(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDirectDialer_DialContext_DoHResolve(t *testing.T) {
	dohURL := dohFixture(t, nil)
	port := tcpEchoListener(t)

	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: loopbackIface(),
	}, nil)
	if err != nil {
		t.Fatalf("NewDirectDialer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("example.com:%d", port))
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	conn.Close()
}

func TestDirectDialer_DialContext_IPLiteralSkipsDoH(t *testing.T) {
	// DoH 端点指向一个会失败的 handler;IP 字面量目标必须完全不发 DoH 请求。
	dohURL := dohFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("DoH queried for IP-literal target: %s", r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	})
	port := tcpEchoListener(t)

	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: loopbackIface(),
	}, nil)
	if err != nil {
		t.Fatalf("NewDirectDialer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	conn.Close()
}

func TestDirectDialer_DoHFailure_StrictNoFallback(t *testing.T) {
	// DoH 服务 500:严格模式必须报错,绝不静默退化为系统 resolver。
	// 若退化为系统 resolver,example.com 会被真实解析并给出连接类错误(非 doh 错误)。
	dohURL := dohFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	port := tcpEchoListener(t)

	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: loopbackIface(),
	}, nil)
	if err != nil {
		t.Fatalf("NewDirectDialer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = d.DialContext(ctx, "tcp", fmt.Sprintf("example.com:%d", port))
	if err == nil {
		t.Fatal("DialContext() expected error when DoH fails, got nil")
	}
	if !strings.Contains(err.Error(), "direct egress") {
		t.Errorf("error = %v, want direct-egress wrapped DoH failure (no silent fallback)", err)
	}
}

func TestDirectDialer_DoHNXDOMAIN(t *testing.T) {
	dohURL := dohFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := new(dns.Msg)
		if err := q.Unpack(body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := new(dns.Msg)
		resp.SetReply(q)
		resp.Rcode = dns.RcodeNameError
		packed, _ := resp.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(packed)
	})

	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: loopbackIface(),
	}, nil)
	if err != nil {
		t.Fatalf("NewDirectDialer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := d.DialContext(ctx, "tcp", "example.com:443"); err == nil {
		t.Fatal("DialContext() expected error on NXDOMAIN, got nil")
	}
}

func TestNewDirectDialer_BadInterface(t *testing.T) {
	dohURL := dohFixture(t, nil)
	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: "noexist0",
	}, nil)
	if !bindStrictPlatform {
		// Linux/其他尽力语义:绑定不可用降级为仅 DoH(warn 日志),构造不报错。
		if err != nil {
			t.Fatalf("NewDirectDialer() error = %v, want nil under DoH-only degrade", err)
		}
		if d == nil {
			t.Fatal("NewDirectDialer() dialer = nil under degrade")
		}
		return
	}
	// 严格平台(macOS):网卡名不存在必须构造期报错(不退化)。
	if err == nil {
		t.Fatal("NewDirectDialer() expected error for nonexistent interface, got nil")
	}
	if !strings.Contains(err.Error(), "noexist0") {
		t.Errorf("error = %v, want mention of interface name", err)
	}
}

func TestNewDirectDialer_DoHURLHostMustBeIPLiteral(t *testing.T) {
	// DoH 端点 host 是域名 -> 鸡生蛋二次解析,构造期拒绝。
	_, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    "https://dns.example.com/dns-query",
		Interface: loopbackIface(),
	}, nil)
	if err == nil {
		t.Fatal("NewDirectDialer() expected error for domain DoH host, got nil")
	}
	if !strings.Contains(err.Error(), "IP literal") {
		t.Errorf("error = %v, want IP-literal constraint message", err)
	}
}

func TestDirectDialer_ListPacket_Binds(t *testing.T) {
	dohURL := dohFixture(t, nil)
	d, err := NewDirectDialer(DirectEgressConfig{
		Enabled:   true,
		DoHURL:    dohURL,
		Interface: loopbackIface(),
	}, nil)
	if err != nil {
		t.Fatalf("NewDirectDialer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pc, err := d.ListenPacket(ctx, "udp", "127.0.0.1:0", netip.MustParseAddrPort("127.0.0.1:53"))
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	pc.Close()
}

// TestNewHostResolver_RoundTrip 识别层"只解析" seam:假 DoH 端点 round-trip,
// example.com 解析出 IP 字符串;与拨号器不同,无需网卡绑定。
func TestNewHostResolver_RoundTrip(t *testing.T) {
	dohURL := dohFixture(t, nil)
	resolve, err := NewHostResolver(DirectEgressConfig{Enabled: true, DoHURL: dohURL})
	if err != nil {
		t.Fatalf("NewHostResolver() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := resolve(ctx, "example.com")
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if len(ips) == 0 || ips[0] != "127.0.0.1" {
		t.Errorf("resolve() = %v, want [127.0.0.1]", ips)
	}
}

// TestNewHostResolver_DoHURLHostMustBeIPLiteral 与拨号器同一约束:
// 域名 host 的 DoH 端点构造期拒绝(会落回被劫持的系统 DNS)。
func TestNewHostResolver_DoHURLHostMustBeIPLiteral(t *testing.T) {
	_, err := NewHostResolver(DirectEgressConfig{Enabled: true, DoHURL: "https://dns.example.com/dns-query"})
	if err == nil {
		t.Fatal("NewHostResolver() expected error for domain DoH host, got nil")
	}
	if !strings.Contains(err.Error(), "IP literal") {
		t.Errorf("error = %v, want IP-literal constraint message", err)
	}
}

func TestDetectPhysicalInterface_ExcludesVirtual(t *testing.T) {
	// 自动识别:结果不得是虚拟网卡;沙箱无物理网卡时跳过而非失败。
	iface, err := detectPhysicalInterface()
	if err != nil {
		t.Skipf("no physical interface in this environment: %v", err)
	}
	for _, prefix := range []string{"utun", "lo", "awdl", "llw", "gif", "stf", "bridge"} {
		if strings.HasPrefix(iface, prefix) {
			t.Errorf("detectPhysicalInterface() = %q, must not be virtual (%s*)", iface, prefix)
		}
	}
}
