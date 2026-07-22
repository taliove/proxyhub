package store

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// fullExamReportWithBaseline 构造完整体检报告:稳定性分 + 基准测速行 + 出网段。
func fullExamReportWithBaseline(score int, baselineMbps float64) detection.ExamReport {
	return detection.ExamReport{
		Stability:   &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: score},
		RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{{Code: "baseline", DownMbps: baselineMbps}}},
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.9", CountryCode: "US", Hosting: true},
		},
	}
}

// TestRecomputeNodeTags_StabilityCheckUpdatesStableKeepsFast
// 出网+稳定性任务落库后重算:stable-* 取新鲜评分(90 -> 30,good -> poor),
// fast 标签不被缺段报告误撤(基准行回填自最近完整体检口径)。
func TestRecomputeNodeTags_StabilityCheckUpdatesStableKeepsFast(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"

	// 先有一次完整体检:90 分 + 基准 80Mbps -> stable-good + fast。
	if err := st.SaveExamHistory(key, fullExamReportWithBaseline(90, 80)); err != nil {
		t.Fatalf("save full exam: %v", err)
	}
	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("recompute after full exam: %v", err)
	}
	got, _ := st.ListNodeTags([]string{key})
	wantBefore := []string{"fast", "hosting", "region:US", "stable-good"}
	if !reflect.DeepEqual(got[key], wantBefore) {
		t.Fatalf("tags after full exam = %v, want %v", got[key], wantBefore)
	}

	// 出网+稳定性任务落库:30 分(差档),出网段刷新为 JP 住宅。
	check := detection.ExamReport{
		Source:    detection.ExamSourceStabilityCheck,
		Stability: &detection.StabilityMetrics{Total: 30, Succeeded: 18, Score: 30},
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "198.51.100.9", CountryCode: "JP", Hosting: false},
		},
	}
	if err := st.SaveExamHistory(key, check); err != nil {
		t.Fatalf("save stability check: %v", err)
	}
	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("recompute after stability check: %v", err)
	}

	got, _ = st.ListNodeTags([]string{key})
	// stable-poor 取新鲜分;fast 保留(基准行回填);region/hosting 取新鲜出网段。
	wantAfter := []string{"fast", "region:JP", "residential", "stable-poor"}
	if !reflect.DeepEqual(got[key], wantAfter) {
		t.Fatalf("tags after stability check = %v, want %v", got[key], wantAfter)
	}
}

// TestRecomputeNodeTags_OnlyStabilityCheckHistory
// 节点只有 stability_check 历史:stable-* 照常派生(新鲜分),fast 不出现(无基准行数据源)。
func TestRecomputeNodeTags_OnlyStabilityCheckHistory(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"

	check := detection.ExamReport{
		Source:    detection.ExamSourceStabilityCheck,
		Stability: &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: 95},
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.9", CountryCode: "US", Hosting: true},
		},
	}
	if err := st.SaveExamHistory(key, check); err != nil {
		t.Fatalf("save stability check: %v", err)
	}
	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("recompute: %v", err)
	}

	got, _ := st.ListNodeTags([]string{key})
	want := []string{"hosting", "region:US", "stable-good"}
	if !reflect.DeepEqual(got[key], want) {
		t.Fatalf("tags = %v, want %v (no fast without baseline data)", got[key], want)
	}
}
