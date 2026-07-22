package detection

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// stabilityCheckNode fixture:全零 UUID + example.com,绝不含真实凭证。
func stabilityCheckNode() *subscription.Node {
	return &subscription.Node{
		Name:   "stability-node",
		Type:   "vmess",
		Server: "example.com",
		Port:   443,
		UUID:   "00000000-0000-0000-0000-000000000000",
	}
}

// newStabilityCheckDetector 构造出网+稳定性检查专用 Detector:出网/稳定性注入假探测器
// (不触真实网络),多地域/解锁工厂设为"被调用即失败"——本动作不得触达这两段。
func newStabilityCheckDetector(t *testing.T) *Detector {
	t.Helper()
	d := NewDetector(1, time.Second, time.Second)
	d.SetExamConfigProvider(func() ExamConfig {
		return ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 50, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 5}
	})
	d.stabilityProbeFactory = func(*subscription.Node) (StabilityProbe, error) {
		return func(context.Context) (int, bool) { return 42, true }, nil
	}
	d.egressProbeFactory = func(*subscription.Node) (EgressProbe, error) {
		return EgressProbe{
			IPv4: func(context.Context) EgressIPv4 {
				return EgressIPv4{IP: "203.0.113.7", Country: "United States", CountryCode: "US", Hosting: true}
			},
			IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
			DNS: func(context.Context) EgressDNS {
				return EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example DNS"}
			},
		}, nil
	}
	d.regionSpeedProbeFactory = func(*subscription.Node) (RegionSpeedProbe, error) {
		t.Fatal("region speed factory must not be called by egress+stability check")
		return nil, errors.New("unreachable")
	}
	d.unlockProbeFactory = func(*subscription.Node) (UnlockProbe, error) {
		t.Fatal("unlock factory must not be called by egress+stability check")
		return nil, errors.New("unreachable")
	}
	return d
}

// 出网+稳定性检查:只跑出网画像 + 稳定性采样两段,报告带来源标记,无测速/解锁段。
func TestExamStreamEgressStability_RunsEgressAndStabilityOnly(t *testing.T) {
	d := newStabilityCheckDetector(t)

	var phases []string
	report := d.ExamStreamEgressStability(context.Background(), stabilityCheckNode(), func(e ExamEvent) {
		phases = append(phases, e.Phase)
	})

	if report.Egress == nil {
		t.Error("report.Egress = nil, want egress profile section")
	}
	if report.Stability == nil {
		t.Fatal("report.Stability = nil, want stability section")
	}
	if report.Stability.Total == 0 {
		t.Error("report.Stability.Total = 0, want samples collected")
	}
	if report.RegionSpeed != nil {
		t.Error("report.RegionSpeed != nil, want no speed section")
	}
	if report.Unlock != nil {
		t.Error("report.Unlock != nil, want no unlock section")
	}
	if report.Source != ExamSourceStabilityCheck {
		t.Errorf("report.Source = %q, want %q", report.Source, ExamSourceStabilityCheck)
	}

	// 事件流应含稳定性采样帧与 done 终帧。
	var sawSample, sawDone bool
	for _, p := range phases {
		switch p {
		case "sample":
			sawSample = true
		case "done":
			sawDone = true
		}
	}
	if !sawSample {
		t.Error("no sample event emitted, want stability samples streamed")
	}
	if !sawDone {
		t.Error("no done event emitted, want terminal frame")
	}
}

// 建会话失败:返回的报告仍带来源标记(供调用方识别口径),但无稳定性段(不落历史)。
func TestExamStreamEgressStability_SessionFailureMarksSource(t *testing.T) {
	d := newStabilityCheckDetector(t)
	d.stabilityProbeFactory = func(*subscription.Node) (StabilityProbe, error) {
		return nil, errors.New("create proxy session failed")
	}

	report := d.ExamStreamEgressStability(context.Background(), stabilityCheckNode(), func(ExamEvent) {})

	if report.Source != ExamSourceStabilityCheck {
		t.Errorf("report.Source = %q, want %q", report.Source, ExamSourceStabilityCheck)
	}
	if report.Stability != nil {
		t.Error("report.Stability != nil on session failure, want nil (no history persisted)")
	}
}

// 来源标记经 JSON 序列化保留:stability_check 报告带 source 字段,完整体检省略(omitempty)。
func TestExamReport_SourceJSONRoundTrip(t *testing.T) {
	check := ExamReport{Source: ExamSourceStabilityCheck, Stability: &StabilityMetrics{Total: 10, Score: 88}}
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"source":"stability_check"`) {
		t.Errorf("stability check report JSON missing source marker, got %s", data)
	}
	var back ExamReport
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Source != ExamSourceStabilityCheck {
		t.Errorf("round-trip Source = %q, want %q", back.Source, ExamSourceStabilityCheck)
	}

	full := ExamReport{Stability: &StabilityMetrics{Total: 10, Score: 88}}
	data, err = json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if strings.Contains(string(data), "source") {
		t.Errorf("full exam report JSON must omit source key, got %s", data)
	}
}
