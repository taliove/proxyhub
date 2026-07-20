package detection

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestTestNode_QuickReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	d := NewDetector(4, time.Second, time.Second)
	node := &subscription.Node{Server: "127.0.0.1", Port: port, Type: "vless"}
	res := d.TestNode(context.Background(), node, "quick")
	if !res.Available {
		t.Errorf("quick test on open port should be available, got %+v", res)
	}
	if res.Mode != "quick" {
		t.Errorf("Mode = %q, want quick", res.Mode)
	}
}

func TestTestNode_QuickUnreachable(t *testing.T) {
	d := NewDetector(4, 300*time.Millisecond, time.Second)
	// 未监听的端口
	node := &subscription.Node{Server: "127.0.0.1", Port: 1, Type: "vless"}
	res := d.TestNode(context.Background(), node, "quick")
	if res.Available {
		t.Errorf("quick test on closed port should be unavailable, got %+v", res)
	}
	if res.Error == "" {
		t.Error("expected error message on unreachable")
	}
}
