package server

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestSlotModeDeliverable 槽位模式:只下发槽位挂载节点,按槽位名排序,
// 无槽位节点不进;stale/屏蔽剔除;条件/关键词不参与
func TestSlotModeDeliverable(t *testing.T) {
	named1 := &subscription.Node{Name: "原名甲", Type: "ss", Cipher: "aes-128-gcm", Password: "p",
		Server: "a.example.com", Port: 443, Source: "机场A", Available: true}
	named2 := &subscription.Node{Name: "原名乙", Type: "ss", Cipher: "aes-128-gcm", Password: "p",
		Server: "b.example.com", Port: 443, Source: "机场A", Available: true}
	unnamed := &subscription.Node{Name: "无名", Type: "ss", Cipher: "aes-128-gcm", Password: "p",
		Server: "c.example.com", Port: 443, Source: "机场A", Available: true}
	srv, st := newTestServer(t, []*subscription.Node{unnamed, named2, named1})

	// 两个槽位(码位序:美国主力 < 香港主力)
	if _, err := st.CreateNameSlotForUser(0, "美国主力", named2.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNameSlotForUser(0, "香港主力", named1.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	ep, err := st.CreateEndpointForUser(0, "槽位订阅")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEndpointSlotModeForUser(0, ep.ID, true); err != nil {
		t.Fatal(err)
	}
	ep, _ = st.GetEndpointByID(ep.ID)

	out := srv.endpointDeliverableNodes(ep)
	if len(out) != 2 {
		t.Fatalf("slot mode delivered = %d, want 2 (无名节点不进)", len(out))
	}
	// 按槽位名码位序(确定性,转移后布局不变):美(U+7F8E) < 香(U+9999)
	if out[0].EffectiveName() != "美国主力" || out[1].EffectiveName() != "香港主力" {
		t.Errorf("order/names = %q,%q", out[0].EffectiveName(), out[1].EffectiveName())
	}

	// 屏蔽后不下发
	if err := st.BlockNodeForUser(0, named1.NodeKey()); err != nil {
		t.Fatal(err)
	}
	out = srv.endpointDeliverableNodes(ep)
	if len(out) != 1 || out[0].EffectiveName() != "美国主力" {
		t.Errorf("after block = %d nodes, want only 美国主力", len(out))
	}
}

// TestSlotModeEndToEnd /sub 真实链路:槽位模式输出只含槽位名节点
func TestSlotModeEndToEnd(t *testing.T) {
	named := &subscription.Node{Name: "原名", Type: "ss", Cipher: "aes-128-gcm", Password: "p",
		Server: "a.example.com", Port: 443, Source: "机场A", Available: true}
	unnamed := &subscription.Node{Name: "路人", Type: "ss", Cipher: "aes-128-gcm", Password: "p",
		Server: "d.example.com", Port: 443, Source: "机场A", Available: true}
	srv, st := newTestServer(t, []*subscription.Node{named, unnamed})
	h := srv.Handler()
	if _, err := st.CreateNameSlotForUser(0, "我的主力", named.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	ep, _ := st.CreateEndpointForUser(0, "e2e")
	if err := st.SetEndpointSlotModeForUser(0, ep.ID, true); err != nil {
		t.Fatal(err)
	}

	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "我的主力") {
		t.Error("slot node should be delivered with slot name")
	}
	if strings.Contains(out, "路人") {
		t.Error("non-slot node must not appear in slot mode")
	}
}

// TestSlotModeEmptyGives503 无槽位/全空槽时 503(不伪装有内容)
func TestSlotModeEmptyGives503(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	ep, _ := st.CreateEndpointForUser(0, "empty")
	if err := st.SetEndpointSlotModeForUser(0, ep.ID, true); err != nil {
		t.Fatal(err)
	}
	status, _ := fetchSubStatus(t, h, ep)
	if status != 503 {
		t.Errorf("empty slot mode = %d, want 503", status)
	}
}
