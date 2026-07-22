package aggregator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// newKindTestNode 构造池内单节点并直接挂到聚合器上(绕过拉取)。
func newKindTestNode(server string, port int) *subscription.Node {
	return &subscription.Node{
		Name: "n", Type: "ss", Server: server, Port: port,
		Cipher: "aes-256-gcm", Password: "p", Source: "机场A",
	}
}

// TestUpdateNodeTestResult_DetectionKind 即时测试写回判定来源:
// quick(TCP 快检) -> health;real(真实代理检测) -> real;bandwidth 不动来源。
func TestUpdateNodeTestResult_DetectionKind(t *testing.T) {
	agg, _ := newTestAggregator(t)
	node := newKindTestNode("10.0.0.1", 8388)
	agg.nodes = []*subscription.Node{node}
	key := node.NodeKey()

	if !agg.UpdateNodeTestResult(key, "quick", true, 100, 0, 0, "", "") {
		t.Fatal("UpdateNodeTestResult(quick) 未命中节点")
	}
	// 写回为不可变语义(浅拷贝替换池内对象),断言须读池内当前节点而非旧指针
	if got := agg.nodes[0].DetectionKind; got != subscription.DetectionKindHealth {
		t.Errorf("quick 后 DetectionKind = %q, want %q", got, subscription.DetectionKindHealth)
	}

	if !agg.UpdateNodeTestResult(key, "real", true, 200, 0, 0, "", "") {
		t.Fatal("UpdateNodeTestResult(real) 未命中节点")
	}
	if got := agg.nodes[0].DetectionKind; got != subscription.DetectionKindReal {
		t.Errorf("real 后 DetectionKind = %q, want %q", got, subscription.DetectionKindReal)
	}

	// bandwidth 只更新带宽字段,不得改变判定来源
	if !agg.UpdateNodeTestResult(key, "bandwidth", true, 200, 50, 10, "", "") {
		t.Fatal("UpdateNodeTestResult(bandwidth) 未命中节点")
	}
	if got := agg.nodes[0].DetectionKind; got != subscription.DetectionKindReal {
		t.Errorf("bandwidth 后 DetectionKind = %q, want %q(不应被改动)", got, subscription.DetectionKindReal)
	}
}

// TestUpdateNodeTestResult_QuickAfterReal 最近一次判定是 TCP 快检时,来源如实回落为 health:
// quick 会覆盖 Available,若仍标 real 会误导排障(机场测试抽样检活正是 quick)。
func TestUpdateNodeTestResult_QuickAfterReal(t *testing.T) {
	agg, _ := newTestAggregator(t)
	node := newKindTestNode("10.0.0.2", 8388)
	agg.nodes = []*subscription.Node{node}
	key := node.NodeKey()

	agg.UpdateNodeTestResult(key, "real", true, 200, 0, 0, "", "")
	agg.UpdateNodeTestResult(key, "quick", false, 0, 0, 0, "timeout", "dial tcp: i/o timeout")
	if got := agg.nodes[0].DetectionKind; got != subscription.DetectionKindHealth {
		t.Errorf("real 后再 quick,DetectionKind = %q, want %q(以最近一次判定为准)", got, subscription.DetectionKindHealth)
	}
}

// TestUpdateNodeTestResult_FailReason 失败原因写回(ticket 0017):
// 失败时记录分类与截断详情;随后检测成功必须清空,不残留旧失败误导排障。
func TestUpdateNodeTestResult_FailReason(t *testing.T) {
	agg, _ := newTestAggregator(t)
	node := newKindTestNode("10.0.0.3", 8388)
	agg.nodes = []*subscription.Node{node}
	key := node.NodeKey()

	// 失败:记录分类与详情
	longDetail := strings.Repeat("x", 500)
	agg.UpdateNodeTestResult(key, "real", false, 0, 0, 0, "timeout", longDetail)
	if got := agg.nodes[0].DetectionFailReason; got != "timeout" {
		t.Errorf("失败后 DetectionFailReason = %q, want timeout", got)
	}
	if got := len([]rune(agg.nodes[0].DetectionFailDetail)); got != 200 {
		t.Errorf("详情未截断到 200 字符, len = %d", got)
	}

	// 成功:清空原因
	agg.UpdateNodeTestResult(key, "real", true, 120, 0, 0, "", "")
	if n := agg.nodes[0]; n.DetectionFailReason != "" || n.DetectionFailDetail != "" {
		t.Errorf("成功后失败原因未清空: reason=%q detail=%q", n.DetectionFailReason, n.DetectionFailDetail)
	}

	// bandwidth 模式不动失败原因
	agg.UpdateNodeTestResult(key, "real", false, 0, 0, 0, "refused", "connection refused")
	agg.UpdateNodeTestResult(key, "bandwidth", true, 0, 50, 10, "", "")
	if got := agg.nodes[0].DetectionFailReason; got != "refused" {
		t.Errorf("bandwidth 后 DetectionFailReason = %q, want refused(不应被改动)", got)
	}
}

// TestCheckHealth_MarksDetectionKind 健康检查(TCP 快检)给从未检测的节点标 health;
// 已做过真实检测的节点不被降级(健康检查不覆盖它们的 Available,来源也不动)。
func TestCheckHealth_MarksDetectionKind(t *testing.T) {
	agg, _ := newTestAggregator(t)

	neverChecked := newKindTestNode("127.0.0.1", 1) // 不可达,TCP 必失败
	realChecked := newKindTestNode("127.0.0.1", 2)
	realChecked.DetectionKind = subscription.DetectionKindReal
	realChecked.DetectionLastCheck = time.Now().Add(-1 * time.Hour) // 非零:做过真实检测
	realChecked.Available = true

	rl := &runLog{} // runID=0 -> no-op 记录器
	agg.checkHealth(context.Background(), rl, []*subscription.Node{neverChecked, realChecked})

	if neverChecked.DetectionKind != subscription.DetectionKindHealth {
		t.Errorf("从未检测节点健康检查后 DetectionKind = %q, want %q", neverChecked.DetectionKind, subscription.DetectionKindHealth)
	}
	// TCP 必失败的节点应记录失败原因分类(ticket 0017):127.0.0.1:1 拒绝连接
	if neverChecked.DetectionFailReason == "" {
		t.Errorf("TCP 失败节点 DetectionFailReason 为空, want 非空分类(预期 refused)")
	}
	if realChecked.DetectionKind != subscription.DetectionKindReal {
		t.Errorf("真实检测节点健康检查后 DetectionKind = %q, want %q(不得降级)", realChecked.DetectionKind, subscription.DetectionKindReal)
	}
}
