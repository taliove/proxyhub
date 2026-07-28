package healthcheck

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// loopbackIface 回环网卡名(回环目标跳过绑定,仅为满足拨号器构造的显式网卡要求)。
func loopbackIface() string {
	if runtime.GOOS == "darwin" {
		return "lo0"
	}
	return "lo"
}

// dohFixture 起一个 mock DoH 服务(RFC 8484 wireformat),把 example.com 解析为 127.0.0.1。
// 返回 DoH URL(IP 字面量 host,满足拨号器的字面量约束)。
func dohFixture(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// tcpListener 起一个本地 TCP 监听(接受即关闭),返回端口。
func tcpListener(t *testing.T) int {
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

// TestCheck_DirectEgressEnabled 开关开:健康检查两段(延迟测速 + 真实请求占位)
// 均经直连拨号器,节点域名 example.com 由自带 DoH(mock)解析为 127.0.0.1,
// 不经过系统 resolver(TUN 下不对假 IP 假通)。
func TestCheck_DirectEgressEnabled(t *testing.T) {
	dohURL := dohFixture(t)
	port := tcpListener(t)

	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: loopbackIface()}
	})

	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	result := checker.Check(context.Background(), node)
	if !result.Available {
		t.Fatalf("Available = false, want true (error: %v)", result.Error)
	}
	if result.Latency <= 0 {
		t.Errorf("Latency = %d, want > 0", result.Latency)
	}
}

// TestCheck_DirectEgressDisabled 开关关:健康检查恢复系统拨号(现状行为),
// 不构造直连拨号器(配置中的坏 DoH/网卡不应生效),IP 字面量节点照常连通。
func TestCheck_DirectEgressDisabled(t *testing.T) {
	port := tcpListener(t)

	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: false, DoHURL: "http://127.0.0.1:1/dns-query", Interface: "noexist0"}
	})

	node := &subscription.Node{Name: "n", Server: "127.0.0.1", Port: port}
	result := checker.Check(context.Background(), node)
	if !result.Available {
		t.Fatalf("Available = false with disabled direct egress, want true (error: %v)", result.Error)
	}
}

// TestCheck_NoProvider 未注入 provider:保持系统拨号(测试/装配缺省路径)。
func TestCheck_NoProvider(t *testing.T) {
	port := tcpListener(t)

	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	node := &subscription.Node{Name: "n", Server: "127.0.0.1", Port: port}
	result := checker.Check(context.Background(), node)
	if !result.Available {
		t.Fatalf("Available = false without provider, want true (error: %v)", result.Error)
	}
}

// TestCheck_DirectEgressBadInterface 网卡不存在时按平台语义分流:
// 严格平台(macOS)报错原样上抛到 Result.Error,节点判不可用,绝不静默退化
// 为系统拨号假性可用;尽力平台(Linux/其他)降级为仅 DoH,检查照常通过。
func TestCheck_DirectEgressBadInterface(t *testing.T) {
	dohURL := dohFixture(t)
	port := tcpListener(t)

	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: "noexist0"}
	})

	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	result := checker.Check(context.Background(), node)
	if !detection.BindStrictPlatform() {
		if !result.Available {
			t.Fatalf("Available = false, want true under DoH-only degrade (error: %v)", result.Error)
		}
		return
	}
	if result.Available {
		t.Fatal("Available = true, want false under strict egress failure (no fake-available fallback)")
	}
	if result.Error == nil {
		t.Fatal("Error = nil, want strict direct-egress error")
	}
	if !strings.Contains(result.Error.Error(), "direct egress") {
		t.Errorf("Error = %v, want direct-egress wrapped", result.Error)
	}
	if !strings.Contains(result.Error.Error(), "latency check") {
		t.Errorf("Error = %v, want latency-check stage prefix", result.Error)
	}
}

// TestCheck_DirectEgressConfigHotRead provider 每次检查实时读取:
// 运行中从关切到开,无需重建 checker 即生效(settings 热改语义)。
func TestCheck_DirectEgressConfigHotRead(t *testing.T) {
	dohURL := dohFixture(t)
	port := tcpListener(t)

	enabled := false
	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: enabled, DoHURL: dohURL, Interface: loopbackIface()}
	})

	// 关:example.com 走系统 resolver,通常解析不到 127.0.0.1 -> 不可用;
	// 不依赖该行为(系统 DNS 环境相关),只验切换后直连分支生效。
	enabled = true
	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	result := checker.Check(context.Background(), node)
	if !result.Available {
		t.Fatalf("Available = false after hot enable, want true (error: %v)", result.Error)
	}
}

// TestCheckerDirectDialer_Memoized 拨号器按配置记忆化:配置相同复用同一实例
// (DoH 缓存跨节点命中、TLS 连接复用),配置变才重建。
func TestCheckerDirectDialer_Memoized(t *testing.T) {
	dohURL1 := dohFixture(t)
	dohURL2 := dohFixture(t)
	current := detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL1, Interface: loopbackIface()}

	checker := NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig { return current })

	d1, err := checker.directDialer()
	if err != nil {
		t.Fatalf("directDialer() error = %v", err)
	}
	d2, err := checker.directDialer()
	if err != nil {
		t.Fatalf("directDialer() error = %v", err)
	}
	if d1 != d2 {
		t.Error("directDialer() with unchanged config returned different instances, want reuse")
	}

	current = detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL2, Interface: loopbackIface()}
	d3, err := checker.directDialer()
	if err != nil {
		t.Fatalf("directDialer() after config change error = %v", err)
	}
	if d3 == d1 {
		t.Error("directDialer() after config change returned old instance, want rebuild")
	}
}
