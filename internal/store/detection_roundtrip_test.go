package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

func TestDetectionResultsRoundtrip(t *testing.T) {
	st := newTestStore(t)

	// 写入一个节点对多个目标的检测结果
	results := []detection.Result{
		{NodeKey: "1.2.3.4:443", TargetName: "connectivity", Available: true, Latency: 50, Error: ""},
		{NodeKey: "1.2.3.4:443", TargetName: "OpenAI", Available: false, Latency: 0, Error: "blocked: found 'unsupported_country'"},
		{NodeKey: "1.2.3.4:443", TargetName: "YouTube", Available: true, Latency: 120, Error: ""},
	}
	if err := st.SaveDetectionResults(results, "TestNode", "TestAirport"); err != nil {
		t.Fatalf("SaveDetectionResults failed: %v", err)
	}

	// 读回来
	got, err := st.GetLatestDetectionResults([]string{"1.2.3.4:443"})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults failed: %v", err)
	}

	views := got["1.2.3.4:443"]
	if len(views) != 3 {
		t.Fatalf("expected 3 target results, got %d", len(views))
	}

	// 验证 OpenAI 的失败原因被正确保留
	var openaiFound bool
	for _, v := range views {
		if v.TargetName == "OpenAI" {
			openaiFound = true
			if v.Available {
				t.Error("OpenAI should be unavailable")
			}
			if v.Error != "blocked: found 'unsupported_country'" {
				t.Errorf("OpenAI error not preserved: got %q", v.Error)
			}
		}
	}
	if !openaiFound {
		t.Error("OpenAI target result not found")
	}
}
