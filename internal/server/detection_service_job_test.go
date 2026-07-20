package server

import (
	"context"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestDetectionService_TriggerDetection_JobsBased verifies trigger starts a job.
func TestDetectionService_TriggerDetection_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}
	targets := []detection.Target{{Name: "connectivity", URL: "http://example.com"}}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store for in-memory only test
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return targets, nil },
		func(context.Context, *subscription.Node, []detection.Target) []detection.Result {
			return []detection.Result{{Available: true}}
		},
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return targets, nil },
	)

	// First trigger should succeed
	err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"})
	if err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// Second trigger should fail (already running)
	err = svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"})
	if err == nil {
		t.Error("TriggerDetection() while running = nil, want error")
	}
}

// TestDetectionService_CancelDetection_JobsBased verifies cancel stops the job.
func TestDetectionService_CancelDetection_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}

	// detectNode 尊重 ctx:取消后探测立刻返回,任务才能收口(生产实现同理)。
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		<-ctx.Done()
		return []detection.Result{{Available: false, Error: "cancelled"}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Start detection
	if err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"}); err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// Cancel should succeed
	err := svc.CancelDetection()
	if err != nil {
		t.Errorf("CancelDetection() = %v, want nil", err)
	}

	// 等任务真正收口(状态翻转为非 running),再取消应报"无运行中任务"。
	waitForStatus(t, svc, false)

	err = svc.CancelDetection()
	if err == nil {
		t.Error("CancelDetection() when not running = nil, want error")
	}
}

// waitForStatus 轮询直至任务 running 状态变为 want(或超时失败)。
func waitForStatus(t *testing.T, svc *DetectionServiceJobs, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if svc.GetStatus().Running == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status.Running did not become %v within 3s", want)
}

// TestDetectionService_GetStatus_JobsBased verifies status reporting.
func TestDetectionService_GetStatus_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
	}

	var processedCount int
	gate := make(chan struct{}) // 第一个节点在 gate 前等待,保证"检测进行中"可被确定性观测
	doneCh := make(chan struct{})
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		processedCount++
		if processedCount == 1 {
			<-gate // 阻塞任务直到测试放行(此时任务必然处于 running)
		}
		if processedCount == 2 {
			close(doneCh)
		}
		return []detection.Result{{Available: true}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Before trigger: not running
	status := svc.GetStatus()
	if status.Running {
		t.Error("GetStatus() before trigger: Running = true, want false")
	}

	// Start detection
	if err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"}); err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// During detection: running(第一个节点被 gate 阻塞,任务必然仍在进行)
	waitForStatus(t, svc, true)
	status = svc.GetStatus()
	if !status.Running {
		t.Error("GetStatus() during detection: Running = false, want true")
	}
	if status.TotalNodes != 2 {
		t.Errorf("GetStatus() TotalNodes = %d, want 2", status.TotalNodes)
	}

	// 放行并完成检测
	close(gate)

	// Wait for completion
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("detection did not complete in time")
	}

	// After completion: not running
	waitForStatus(t, svc, false)
}

// TestDetectionService_ScopeSelection verifies node scope filtering.
func TestDetectionService_ScopeSelection(t *testing.T) {
	nodes := []*subscription.Node{
		// NodeKey 由 server:port 构成,必须互异,否则 selected 范围匹配会把同 key 节点全选进来。
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 444, UUID: "00000000-0000-0000-0000-000000000001"},
		{Name: "node3", Type: "vmess", Server: "example.com", Port: 445, UUID: "00000000-0000-0000-0000-000000000002"},
	}

	var detectedNodes []*subscription.Node
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		detectedNodes = append(detectedNodes, node)
		return []detection.Result{{Available: true}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Test selected scope
	detectedNodes = nil
	scope := DetectionScope{
		Type:     "selected",
		NodeKeys: []string{nodes[0].NodeKey(), nodes[2].NodeKey()},
	}

	if err := svc.TriggerDetection(context.Background(), scope); err != nil {
		t.Fatalf("TriggerDetection(selected) = %v, want nil", err)
	}

	// 等任务收口再断言:append 发生在 job goroutine,状态翻转在其后( happens-before ),无数据竞争。
	waitForStatus(t, svc, false)

	if len(detectedNodes) != 2 {
		t.Errorf("detected %d nodes with selected scope, want 2", len(detectedNodes))
	}
}
