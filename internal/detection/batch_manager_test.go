package detection_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestBatchDetectionManager_TriggerRecordsScope 验证触发范围标记随 params 持久化,
// 供任务中心生成可读范围标识(全部/选中)。
func TestBatchDetectionManager_TriggerRecordsScope(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	nodes := []*subscription.Node{
		{Name: "n1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}
	mgr := detection.NewBatchDetectionManager(
		st.Jobs(),
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) {
			return []detection.Target{{Name: "t", URL: "http://example.com"}}, nil
		},
		func(context.Context, *subscription.Node, []detection.Target) []detection.Result {
			return []detection.Result{{Available: true}}
		},
		func(*subscription.Node, []detection.Result) {},
	)

	if err := mgr.Trigger([]string{nodes[0].NodeKey()}, "selected"); err != nil {
		t.Fatalf("Trigger() = %v, want nil", err)
	}

	// 等任务收口,保证终态落库
	deadline := time.Now().Add(5 * time.Second)
	var recParams json.RawMessage
	for time.Now().Before(deadline) {
		recs, err := st.Jobs().LoadAll()
		if err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		if len(recs) == 1 && recs[0].Status != "running" {
			recParams = recs[0].Params
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if recParams == nil {
		t.Fatal("job did not finish in time")
	}

	var p struct {
		NodeKeys []string `json:"node_keys"`
		Scope    string   `json:"scope"`
	}
	if err := json.Unmarshal(recParams, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Scope != "selected" {
		t.Errorf("params.Scope = %q, want %q", p.Scope, "selected")
	}
	if len(p.NodeKeys) != 1 {
		t.Errorf("params.NodeKeys len = %d, want 1", len(p.NodeKeys))
	}
}
