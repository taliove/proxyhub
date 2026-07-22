package server

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
	"github.com/taliove/proxyhub/internal/healthcheck"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 机场测试抽样检活路径(HealthCheckAdapter 复用 healthcheck.Checker)的直连出口回归:
// 与检测主链路同一开关,开关开时抽样检活同样直连节点真实 IP,不对假 IP 假通。

func adapterLoopbackIface() string {
	if runtime.GOOS == "darwin" {
		return "lo0"
	}
	return "lo"
}

// adapterDoHFixture mock DoH(RFC 8484 wireformat):example.com -> 127.0.0.1。
func adapterDoHFixture(t *testing.T) string {
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

func adapterTCPListener(t *testing.T) int {
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

// TestHealthCheckAdapter_DirectEgressEnabled 开关开:抽样检活经直连拨号器,
// example.com 由自带 DoH(mock)解析为 127.0.0.1 后连通,结果可用。
func TestHealthCheckAdapter_DirectEgressEnabled(t *testing.T) {
	dohURL := adapterDoHFixture(t)
	port := adapterTCPListener(t)

	checker := healthcheck.NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: adapterLoopbackIface()}
	})
	adapter := NewHealthCheckAdapter(checker)

	nodes := []*subscription.Node{{Name: "n", Server: "example.com", Port: port}}
	results := adapter.CheckAll(context.Background(), nodes)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Available {
		t.Fatalf("Available = false, want true (error: %v)", results[0].Error)
	}
}

// TestHealthCheckAdapter_DirectEgressStrictError 装配失败(网卡不存在):
// 抽样检活报错透出到 HealthCheckResult.Error,不假性可用。
func TestHealthCheckAdapter_DirectEgressStrictError(t *testing.T) {
	dohURL := adapterDoHFixture(t)
	port := adapterTCPListener(t)

	checker := healthcheck.NewChecker(3*time.Second, 3*time.Second, "https://example.com", 2)
	checker.SetDirectEgressConfigProvider(func() detection.DirectEgressConfig {
		return detection.DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: "noexist0"}
	})
	adapter := NewHealthCheckAdapter(checker)

	nodes := []*subscription.Node{{Name: "n", Server: "example.com", Port: port}}
	results := adapter.CheckAll(context.Background(), nodes)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Available {
		t.Fatal("Available = true, want false under strict egress failure")
	}
	if results[0].Error == nil || !strings.Contains(results[0].Error.Error(), "direct egress") {
		t.Errorf("Error = %v, want direct-egress wrapped", results[0].Error)
	}
}
