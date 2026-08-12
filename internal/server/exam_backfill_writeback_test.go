package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestOnExamComplete_BackfillWritesNodeHealth 补齐信息收口:source=backfill 的报告
// 把解锁各目标与 connectivity(可用性/延迟)写回 node_health,并把内存池节点的
// 延迟/可用性一并更新——列表「延迟/状态/解锁」列读 node_health,此前体检族不回写
// (用户反馈"补齐后延迟仍一道杠"的根因)。
func TestOnExamComplete_BackfillWritesNodeHealth(t *testing.T) {
	testNode := &subscription.Node{
		Name: "bf-node", Server: "bf.example.com", Port: 443, Source: "机场A",
	}
	s, st := newTestServer(t, []*subscription.Node{testNode})
	key := testNode.NodeKey()

	report := detection.ExamReport{
		Source: detection.ExamSourceBackfill,
		Egress: &detection.EgressMetrics{IPv4: &detection.EgressIPv4{CountryCode: "JP"}},
		Stability: &detection.StabilityMetrics{
			Total: 5, Succeeded: 5, MedianMs: 87, Score: 91,
		},
		Unlock: &detection.UnlockMetrics{
			Results: []detection.Result{
				{TargetName: "Netflix", Available: true, Latency: 120, Level: "full", Region: "JP"},
			},
		},
	}

	s.onExamComplete(0, key, report, 0)

	results, err := st.GetLatestDetectionResults([]string{key})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	conn, ok := results[key]
	if !ok || len(conn) == 0 {
		t.Fatal("node_health has no rows for backfilled node")
	}
	var foundConn, foundUnlock bool
	for _, r := range conn {
		if r.TargetName == "connectivity" {
			foundConn = true
			if !r.Available || r.Latency != 87 {
				t.Errorf("connectivity = %+v, want available=true latency=87", r)
			}
		}
		if r.TargetName == "Netflix" {
			foundUnlock = true
			if !r.Available || r.Level != "full" {
				t.Errorf("unlock row = %+v, want available=true level=full", r)
			}
		}
	}
	if !foundConn {
		t.Error("connectivity row missing in node_health")
	}
	if !foundUnlock {
		t.Error("unlock row missing in node_health")
	}

	// 内存池同步:延迟/可用性回写
	pool := s.nodes.Nodes()
	if len(pool) == 0 || pool[0].Latency != 87 || !pool[0].Available {
		t.Errorf("pool node latency/available = %v/%v, want 87/true", pool[0].Latency, pool[0].Available)
	}
}

// TestOnExamComplete_FullExamDoesNotTouchNodeHealth 完整四段体检(source 空)
// 不写 node_health(零回归:既有语义保持)。
func TestOnExamComplete_FullExamDoesNotTouchNodeHealth(t *testing.T) {
	testNode := &subscription.Node{
		Name: "full-node", Server: "full.example.com", Port: 443,
	}
	s, st := newTestServer(t, []*subscription.Node{testNode})
	key := testNode.NodeKey()

	report := detection.ExamReport{
		Egress:    &detection.EgressMetrics{IPv4: &detection.EgressIPv4{CountryCode: "US"}},
		Stability: &detection.StabilityMetrics{Total: 10, Succeeded: 10, MedianMs: 50, Score: 99},
		Unlock:    &detection.UnlockMetrics{Results: []detection.Result{{TargetName: "Netflix", Available: true}}},
	}

	s.onExamComplete(0, key, report, 0)

	results, err := st.GetLatestDetectionResults([]string{key})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	if len(results[key]) != 0 {
		t.Errorf("full exam should not write node_health, got %+v", results[key])
	}
}

// TestOnExamComplete_BackfillDeadNode 死节点负路径(Check H1):出网全失败的 backfill
// 报告(无稳定性段,被编排器短路)也必须把 connectivity=不可用 写回 node_health 与内存池;
// 且不落 exam_history(无稳定性段不落库的旧不变量)。
func TestOnExamComplete_BackfillDeadNode(t *testing.T) {
	testNode := &subscription.Node{
		Name: "dead-node", Server: "dead.example.com", Port: 443, Source: "机场A",
	}
	s, st := newTestServer(t, []*subscription.Node{testNode})
	key := testNode.NodeKey()

	// 出网三项全失败(编排器短路点的形状):IPv4 error + IPv6 不可达且 error + DNS error
	report := detection.ExamReport{
		Source: detection.ExamSourceBackfill,
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{Error: "dial timeout"},
			IPv6: &detection.EgressIPv6{Available: false, Error: "no route"},
			DNS:  &detection.EgressDNS{Error: "resolve failed"},
		},
	}

	s.onExamComplete(0, key, report, 0)

	results, err := st.GetLatestDetectionResults([]string{key})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	var found bool
	for _, r := range results[key] {
		if r.TargetName == "connectivity" {
			found = true
			if r.Available {
				t.Error("dead node connectivity must be available=false")
			}
		}
	}
	if !found {
		t.Fatal("dead node backfill must write connectivity row (否则列表永远空杠)")
	}

	pool := s.nodes.Nodes()
	if len(pool) == 0 || pool[0].Available {
		t.Errorf("pool node available = %v, want false", len(pool) > 0 && pool[0].Available)
	}

	// 无稳定性段不落历史
	hist, err := st.LatestExamHistory(key)
	if err != nil {
		t.Fatalf("LatestExamHistory: %v", err)
	}
	if hist != nil {
		t.Errorf("dead-node backfill must not write exam history, got %+v", hist)
	}
}
