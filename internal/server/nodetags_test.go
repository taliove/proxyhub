package server

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestToNodeViews_TagsPassthrough /nodes 视图透出标签数组;无标签节点为空(omitempty 省略)。
func TestToNodeViews_TagsPassthrough(t *testing.T) {
	tagged := &subscription.Node{Name: "hk01", Server: "a.example.com", Port: 443, Type: "vmess", Source: "airport"}
	bare := &subscription.Node{Name: "hk02", Server: "b.example.com", Port: 443, Type: "vmess", Source: "airport"}
	tags := map[string][]string{
		tagged.NodeKey(): {"nf-full", "region:US", "fast"},
	}

	views := toNodeViews([]*subscription.Node{tagged, bare}, nil, nil, tags, nil)
	if len(views) != 2 {
		t.Fatalf("views len = %d, want 2", len(views))
	}
	if !reflect.DeepEqual(views[0].Tags, []string{"nf-full", "region:US", "fast"}) {
		t.Errorf("tagged node tags = %v", views[0].Tags)
	}
	if len(views[1].Tags) != 0 {
		t.Errorf("bare node tags = %v, want empty", views[1].Tags)
	}
}

// TestToNodeViews_NilTags 标签 map 为 nil(降级路径)不 panic,所有节点零标签。
func TestToNodeViews_NilTags(t *testing.T) {
	node := &subscription.Node{Name: "hk01", Server: "a.example.com", Port: 443, Type: "vmess"}
	views := toNodeViews([]*subscription.Node{node}, nil, nil, nil, nil)
	if len(views[0].Tags) != 0 {
		t.Errorf("nil tags map should yield empty tags, got %v", views[0].Tags)
	}
}

// TestOnExamComplete_RecomputesTags 体检完成回调:落历史 + 重算该节点标签。
func TestOnExamComplete_RecomputesTags(t *testing.T) {
	srv, st := newTestServer(t, nil)
	key := "example.com:443"

	report := detection.ExamReport{
		Stability: &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: 92},
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.1", CountryCode: "SG", Hosting: false},
		},
	}
	srv.onExamComplete(0, key, report, 0)

	// 历史已落。
	entry, err := st.LatestExamHistory(key)
	if err != nil || entry == nil {
		t.Fatalf("exam history not saved: entry=%v err=%v", entry, err)
	}
	// 标签已重算。
	got, _ := st.ListNodeTags([]string{key})
	want := []string{"region:SG", "residential", "stable-good"}
	if !reflect.DeepEqual(got[key], want) {
		t.Fatalf("post-exam tags = %v, want %v", got[key], want)
	}
}

// TestDetectionService_SaveAndRetag 批量检测落库后按节点重算标签。
func TestDetectionService_SaveAndRetag(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetDetectionTargets(detection.DefaultUnlockTargets()); err != nil {
		t.Fatalf("SetDetectionTargets: %v", err)
	}

	node := &subscription.Node{Name: "hk01", Server: "example.com", Port: 443, Type: "vmess", Source: "airport"}
	results := []detection.Result{
		{NodeKey: node.NodeKey(), TargetName: "Netflix", Available: true, Latency: 88, Level: detection.LevelFull},
		{NodeKey: node.NodeKey(), TargetName: "connectivity", Available: true, Latency: 30},
	}
	SaveAndRetag(st, srv.logger, node, results)

	got, _ := st.ListNodeTags([]string{node.NodeKey()})
	if !reflect.DeepEqual(got[node.NodeKey()], []string{"nf-full"}) {
		t.Fatalf("post-detection tags = %v, want [nf-full]", got[node.NodeKey()])
	}

	// node_health 也应写入(不因重算而丢原有落库)。
	var _ *store.Store = st
	latest, err := st.GetLatestDetectionResults([]string{node.NodeKey()})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	if len(latest[node.NodeKey()]) == 0 {
		t.Fatalf("detection results not persisted")
	}
}
