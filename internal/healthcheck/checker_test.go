package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestMeasureLatency_Success(t *testing.T) {
	// 启动一个测试 TCP 服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)

	checker := NewChecker(5*time.Second, 10*time.Second, "https://www.google.com", 10)
	node := &subscription.Node{
		Server: "127.0.0.1",
		Port:   addr.Port,
	}

	latency, err := checker.measureLatency(context.Background(), node)
	if err != nil {
		t.Errorf("measureLatency() error = %v", err)
	}
	if latency <= 0 {
		t.Errorf("latency = %d, want > 0", latency)
	}
}

func TestMeasureLatency_Timeout(t *testing.T) {
	t.Skip("Timeout test is flaky depending on network environment")

	checker := NewChecker(100*time.Millisecond, 10*time.Second, "https://www.google.com", 10)
	// 使用不可路由的私有地址，会触发真正的超时而不是快速拒绝
	node := &subscription.Node{
		Server: "10.255.255.1",
		Port:   9999,
	}

	_, err := checker.measureLatency(context.Background(), node)
	if err == nil {
		t.Error("measureLatency() expected timeout error, got nil")
	}
}

func TestCheck_Available(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)

	checker := NewChecker(5*time.Second, 10*time.Second, "https://www.google.com", 10)
	node := &subscription.Node{
		Name:   "测试节点",
		Server: "127.0.0.1",
		Port:   addr.Port,
	}

	result := checker.Check(context.Background(), node)
	if !result.Available {
		t.Errorf("Available = false, want true (error: %v)", result.Error)
	}
	if result.Latency <= 0 {
		t.Errorf("Latency = %d, want > 0", result.Latency)
	}
}

func TestCheck_Unavailable(t *testing.T) {
	t.Skip("Timeout test is flaky depending on network environment")

	checker := NewChecker(100*time.Millisecond, 200*time.Millisecond, "https://www.google.com", 10)
	node := &subscription.Node{
		Name:   "不可达节点",
		Server: "10.255.255.1",
		Port:   9999,
	}

	result := checker.Check(context.Background(), node)
	if result.Available {
		t.Error("Available = true, want false")
	}
	if result.Error == nil {
		t.Error("Error = nil, want error")
	}
}

func TestCheckAll_Concurrent(t *testing.T) {
	// 启动 3 个测试服务器
	servers := make([]*net.TCPListener, 3)
	for i := range servers {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		defer ln.Close()
		servers[i] = ln.(*net.TCPListener)
	}

	nodes := []*subscription.Node{
		{Name: "Node1", Server: "127.0.0.1", Port: servers[0].Addr().(*net.TCPAddr).Port},
		{Name: "Node2", Server: "127.0.0.1", Port: servers[1].Addr().(*net.TCPAddr).Port},
		{Name: "Node3", Server: "127.0.0.1", Port: servers[2].Addr().(*net.TCPAddr).Port},
	}

	checker := NewChecker(5*time.Second, 10*time.Second, "https://www.google.com", 2)

	start := time.Now()
	results := checker.CheckAll(context.Background(), nodes)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Errorf("len(results) = %d, want 3", len(results))
	}

	for i, result := range results {
		if !result.Available {
			t.Errorf("result[%d].Available = false, want true (error: %v)", i, result.Error)
		}
	}

	// 并发度 2，3 个节点，应该比串行快
	// 串行约需 3 * latency，并发约需 2 * latency
	if elapsed > 500*time.Millisecond {
		t.Logf("CheckAll took %v, may indicate serial execution", elapsed)
	}
}

func TestTestRealRequestViaHTTP(t *testing.T) {
	// 创建一个测试 HTTP 服务器
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer ts.Close()

	checker := NewChecker(5*time.Second, 10*time.Second, ts.URL, 10)

	// 不通过代理直接访问测试服务器（代理测试需要真实代理环境）
	err := checker.testRealRequestViaHTTP(context.Background(), "")
	if err != nil {
		t.Errorf("testRealRequestViaHTTP() error = %v", err)
	}
}
