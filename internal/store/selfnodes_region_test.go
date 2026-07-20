package store

import "testing"

func TestSelfHostedNode_RegionRoundTrip(t *testing.T) {
	st := newTestStore(t) // 见 store_test.go 的既有 helper
	n := &SelfHostedNode{Name: "自建东京", Protocol: "vless", Server: "1.2.3.4", Port: 443, RegionCode: "JP", Enabled: true}
	if err := st.CreateSelfHostedNode(n); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.ListSelfHostedNodes()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].RegionCode != "JP" {
		t.Fatalf("region_code round-trip failed: %+v", got)
	}
}

func TestSelfHostedNode_ToNode_RegionFallback(t *testing.T) {
	// 有 region_code 用之;为空回退 "SELF"
	withRegion := (&SelfHostedNode{Name: "a", Protocol: "vless", Server: "s", Port: 1, RegionCode: "HK"}).ToNode()
	if withRegion.Region != "HK" {
		t.Errorf("Region = %q, want HK", withRegion.Region)
	}
	empty := (&SelfHostedNode{Name: "b", Protocol: "vless", Server: "s", Port: 1}).ToNode()
	if empty.Region != "SELF" {
		t.Errorf("Region = %q, want SELF (fallback)", empty.Region)
	}
}
