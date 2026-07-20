package detection

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestDetector_TCPQuickCheck(t *testing.T) {
	detector := NewDetector(20, 2*time.Second, 12*time.Second)

	// 起一个本地监听器作为"能连通"的确定性目标(不依赖外网)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	node := &subscription.Node{Server: host, Port: port}
	if !detector.tcpQuickCheck(context.Background(), node) {
		t.Errorf("Expected TCP check to pass for local listener %s", ln.Addr())
	}

	// 关闭监听器后,同一端口应连接失败(确定性,不依赖外部不可达地址的超时)
	addr := ln.Addr().String()
	ln.Close()
	closedNode := &subscription.Node{Server: host, Port: port}
	if detector.tcpQuickCheck(context.Background(), closedNode) {
		t.Errorf("Expected TCP check to fail for closed port %s", addr)
	}
}

func TestDetector_DetectTarget_Direct(t *testing.T) {
	t.Skip("Skipping real proxy test - requires valid proxy node")

	detector := NewDetector(20, 5*time.Second, 12*time.Second)

	// 这需要一个真实的节点配置才能测试
	// 占位测试:确保代码结构正确
	node := &subscription.Node{
		Type:   "vmess",
		Server: "example.com",
		Port:   443,
		UUID:   "test-uuid",
	}

	target := Target{
		Name:             "connectivity",
		URL:              "http://www.gstatic.com/generate_204",
		Method:           "GET",
		Headers:          map[string]string{},
		ExpectStatus:     []int{204},
		ResponseContains: []string{},
		ResponseExcludes: []string{},
	}

	// 这会失败因为节点不真实,但验证了代码路径
	results := detector.detectNode(context.Background(), node, []Target{target})
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}
