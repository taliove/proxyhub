package server

import (
	"encoding/json"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestToNodeViews_StabilityScorePassthrough /nodes 视图透出稳定性分:
// 有分的节点带 stability_score;无分的节点省略(omitempty)。分数 0 是合法值,不被省略。
func TestToNodeViews_StabilityScorePassthrough(t *testing.T) {
	scored := &subscription.Node{Name: "hk01", Server: "a.example.com", Port: 443, Type: "vmess", Source: "airport"}
	zero := &subscription.Node{Name: "hk02", Server: "b.example.com", Port: 443, Type: "vmess", Source: "airport"}
	bare := &subscription.Node{Name: "hk03", Server: "c.example.com", Port: 443, Type: "vmess", Source: "airport"}
	scores := map[string]int{
		scored.NodeKey(): 92,
		zero.NodeKey():   0,
	}

	views := toNodeViews([]*subscription.Node{scored, zero, bare}, nil, nil, nil, scores)
	if len(views) != 3 {
		t.Fatalf("views len = %d, want 3", len(views))
	}
	if views[0].StabilityScore == nil || *views[0].StabilityScore != 92 {
		t.Errorf("scored node stability = %v, want 92", views[0].StabilityScore)
	}
	if views[1].StabilityScore == nil || *views[1].StabilityScore != 0 {
		t.Errorf("zero-score node stability = %v, want 0 (0 is valid poor band)", views[1].StabilityScore)
	}
	if views[2].StabilityScore != nil {
		t.Errorf("bare node stability = %v, want nil", views[2].StabilityScore)
	}
}

// TestToNodeViews_NilScores 分数 map 为 nil(降级路径)不 panic,所有节点无分。
func TestToNodeViews_NilScores(t *testing.T) {
	node := &subscription.Node{Name: "hk01", Server: "a.example.com", Port: 443, Type: "vmess"}
	views := toNodeViews([]*subscription.Node{node}, nil, nil, nil, nil)
	if views[0].StabilityScore != nil {
		t.Errorf("nil scores map should yield nil stability, got %v", views[0].StabilityScore)
	}
}

// TestNodeView_StabilityScoreOmitEmpty 无分节点序列化时省略 stability_score 键;分数 0 保留。
func TestNodeView_StabilityScoreOmitEmpty(t *testing.T) {
	zero := 0
	withZero := nodeView{Name: "z", StabilityScore: &zero}
	data, _ := json.Marshal(withZero)
	if !contains(string(data), `"stability_score":0`) {
		t.Errorf("score 0 must serialize as stability_score:0, got %s", data)
	}

	bare := nodeView{Name: "b"}
	data, _ = json.Marshal(bare)
	if contains(string(data), "stability_score") {
		t.Errorf("nil score must omit stability_score key, got %s", data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
