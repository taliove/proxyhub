package detection

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestEgressAllFailed_AllThreeError 三项皆 error(IPv4/IPv6/DNS 全部 Error 非空)→ true。
func TestEgressAllFailed_AllThreeError(t *testing.T) {
	egress := &EgressMetrics{
		IPv4: &EgressIPv4{Error: "请求失败: timeout"},
		IPv6: &EgressIPv6{Available: false, Error: "IPv6 出口探测超时"},
		DNS:  &EgressDNS{Error: "请求失败: connection refused"},
	}
	if !egressAllFailed(egress) {
		t.Error("all three with error should return true")
	}
}

// TestEgressAllFailed_IPv6UnavailableNotError IPv6 不可达(Available=false, Error="")非失败 → false。
func TestEgressAllFailed_IPv6UnavailableNotError(t *testing.T) {
	egress := &EgressMetrics{
		IPv4: &EgressIPv4{Error: "请求失败: timeout"},
		IPv6: &EgressIPv6{Available: false, Error: ""}, // 明确负判定,非 error
		DNS:  &EgressDNS{Error: "请求失败: timeout"},
	}
	if egressAllFailed(egress) {
		t.Error("ipv6 unavailable without error is definite verdict, not failure")
	}
}

// TestEgressAllFailed_OneSuccess 任一成功 → false。
func TestEgressAllFailed_OneSuccess(t *testing.T) {
	tests := []struct {
		name   string
		egress *EgressMetrics
	}{
		{
			name: "ipv4 success",
			egress: &EgressMetrics{
				IPv4: &EgressIPv4{IP: "203.0.113.7", Country: "US"},
				IPv6: &EgressIPv6{Available: false, Error: "timeout"},
				DNS:  &EgressDNS{Error: "timeout"},
			},
		},
		{
			name: "ipv6 available",
			egress: &EgressMetrics{
				IPv4: &EgressIPv4{Error: "timeout"},
				IPv6: &EgressIPv6{Available: true, Address: "2001:db8::1"},
				DNS:  &EgressDNS{Error: "timeout"},
			},
		},
		{
			name: "dns success",
			egress: &EgressMetrics{
				IPv4: &EgressIPv4{Error: "timeout"},
				IPv6: &EgressIPv6{Available: false, Error: "timeout"},
				DNS:  &EgressDNS{ResolverIP: "8.8.8.8", ResolverGeo: "US"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if egressAllFailed(tt.egress) {
				t.Errorf("one success should return false")
			}
		})
	}
}

// TestEgressAllFailed_NilEgress nil egress → false(防御性,不误判)。
func TestEgressAllFailed_NilEgress(t *testing.T) {
	if egressAllFailed(nil) {
		t.Error("nil egress should return false")
	}
	if egressAllFailed(&EgressMetrics{}) {
		t.Error("empty egress should return false")
	}
}

// TestExamOrchestrator_ShortCircuitOnEgressAllFail 出网全失败 → 跳过后续段 + 发终态事件。
func TestExamOrchestrator_ShortCircuitOnEgressAllFail(t *testing.T) {
	var ran []string
	egressStage := examStage{
		name: "egress",
		run: func(_ context.Context, emit func(ExamEvent), report *ExamReport) {
			ran = append(ran, "egress")
			report.Egress = &EgressMetrics{
				IPv4: &EgressIPv4{Error: "timeout"},
				IPv6: &EgressIPv6{Available: false, Error: "timeout"},
				DNS:  &EgressDNS{Error: "timeout"},
			}
			emit(ExamEvent{Phase: "section_done", Section: "egress", Egress: report.Egress})
		},
	}
	stabilityStage := examStage{
		name: "stability",
		run:  func(_ context.Context, _ func(ExamEvent), _ *ExamReport) { ran = append(ran, "stability") },
	}
	regionStage := examStage{
		name: "region_speed",
		run:  func(_ context.Context, _ func(ExamEvent), _ *ExamReport) { ran = append(ran, "region") },
	}
	unlockStage := examStage{
		name: "unlock",
		run:  func(_ context.Context, _ func(ExamEvent), _ *ExamReport) { ran = append(ran, "unlock") },
	}

	orch := &ExamOrchestrator{stages: []examStage{egressStage, stabilityStage, regionStage, unlockStage}}

	var events []ExamEvent
	report := orch.Run(context.Background(), func(e ExamEvent) { events = append(events, e) })

	// 只跑了出网段,后续全跳过。
	want := []string{"egress"}
	if len(ran) != 1 || ran[0] != "egress" {
		t.Errorf("ran = %v, want %v (后续段应被短路)", ran, want)
	}

	// 事件序列:egress section_done + 终态帧(phase=node_unavailable 或 error)+ done。
	if len(events) < 2 {
		t.Fatalf("events = %d, want at least 2 (section_done + terminal + done)", len(events))
	}

	// 第一帧是 egress section_done。
	if events[0].Phase != "section_done" || events[0].Section != "egress" {
		t.Errorf("events[0] = %+v, want egress section_done", events[0])
	}

	// 最后一帧是 done。
	last := events[len(events)-1]
	if last.Phase != "done" {
		t.Errorf("last event phase = %q, want done", last.Phase)
	}

	// 报告只有 Egress,无后续段。
	if report.Egress == nil {
		t.Error("report.Egress should be present")
	}
	if report.Stability != nil || report.RegionSpeed != nil || report.Unlock != nil {
		t.Errorf("report = %+v, 后续段应为 nil(短路)", report)
	}
}

// TestExamOrchestrator_NoShortCircuitWhenPartialSuccess 部分成功(IPv6 不可达但 IPv4/DNS 成功)→ 不短路。
func TestExamOrchestrator_NoShortCircuitWhenPartialSuccess(t *testing.T) {
	var ran []string
	egressStage := examStage{
		name: "egress",
		run: func(_ context.Context, emit func(ExamEvent), report *ExamReport) {
			ran = append(ran, "egress")
			report.Egress = &EgressMetrics{
				IPv4: &EgressIPv4{IP: "203.0.113.7", Country: "US"},
				IPv6: &EgressIPv6{Available: false}, // 不可达非失败
				DNS:  &EgressDNS{ResolverIP: "8.8.8.8"},
			}
			emit(ExamEvent{Phase: "section_done", Section: "egress"})
		},
	}
	stabilityStage := examStage{
		name: "stability",
		run:  func(_ context.Context, _ func(ExamEvent), _ *ExamReport) { ran = append(ran, "stability") },
	}

	orch := &ExamOrchestrator{stages: []examStage{egressStage, stabilityStage}}
	orch.Run(context.Background(), func(ExamEvent) {})

	want := []string{"egress", "stability"}
	if len(ran) != 2 || ran[0] != "egress" || ran[1] != "stability" {
		t.Errorf("ran = %v, want %v (部分成功不短路)", ran, want)
	}
}

// TestExamOrchestrator_NoShortCircuitWhenNoEgress 无出网段(降级场景)→ 不短路。
func TestExamOrchestrator_NoShortCircuitWhenNoEgress(t *testing.T) {
	var ran []string
	stabilityStage := examStage{
		name: "stability",
		run:  func(_ context.Context, _ func(ExamEvent), _ *ExamReport) { ran = append(ran, "stability") },
	}

	orch := &ExamOrchestrator{stages: []examStage{stabilityStage}}
	orch.Run(context.Background(), func(ExamEvent) {})

	if len(ran) != 1 || ran[0] != "stability" {
		t.Errorf("ran = %v, want [stability] (无出网段不触发短路)", ran)
	}
}

// TestExamKind_ShortCircuitNoHistory 短路体检(report.Stability=nil)→ OnComplete 不落历史。
func TestExamKind_ShortCircuitNoHistory(t *testing.T) {
	var saved bool
	onComplete := func(userID int64, _ string, _ ExamReport) { saved = true }

	runner := func(_ context.Context, _ *subscription.Node, emit func(ExamEvent)) ExamReport {
		// 模拟出网全失败短路:只有 Egress,无 Stability。
		report := ExamReport{
			Egress: &EgressMetrics{
				IPv4: &EgressIPv4{Error: "timeout"},
				IPv6: &EgressIPv6{Available: false, Error: "timeout"},
				DNS:  &EgressDNS{Error: "timeout"},
			},
		}
		emit(ExamEvent{Phase: "error", Error: "出网全失败: 节点不可用"})
		emit(ExamEvent{Phase: "done", Report: &report})
		return report
	}

	kind := &examKind{run: runner, onComplete: onComplete}
	params := examParams{NodeKey: "test-node"}
	paramsJSON, _ := json.Marshal(params)

	node := &subscription.Node{}
	kind.nodes.Store(examNodeRef{userID: 0, nodeKey: "test-node"}, node)

	err := kind.Run(context.Background(), paramsJSON, "", func(json.RawMessage) {}, func(string) {})

	// Run 返回 examResult(伪装成 error)。
	res, ok := err.(examResult)
	if !ok {
		t.Fatalf("Run returned %T, want examResult", err)
	}
	if res.report.Stability != nil {
		t.Errorf("short-circuit report.Stability = %+v, want nil", res.report.Stability)
	}

	// 调用 OnComplete 模拟 finalize。
	kind.OnComplete("test-node", err)

	// 短路体检(无 Stability)不应触发 onComplete 回调。
	if saved {
		t.Error("short-circuit exam saved to history, want not saved")
	}
}
