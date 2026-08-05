package store

import "testing"

// 身份查重的多租户口径(issue #53,check 评审:server 测试走 userID=0 逃生舱,
// 生产的 userID>0 分支必须有直接覆盖):同用户同身份撞、跨用户不撞、编辑排除自身。
func TestSelfHostedNodeIdentityExists_MultiTenant(t *testing.T) {
	st := newTestStore(t)

	mk := func(userID int64, name string, port int) *SelfHostedNode {
		return &SelfHostedNode{
			Name: name, Protocol: "vless", Server: "dup.example.com", Port: port,
			UUID: "00000000-0000-0000-0000-000000000000", UserID: userID, Enabled: true,
		}
	}
	if err := st.CreateSelfHostedNodeForUser(1, mk(1, "u1-a", 443)); err != nil {
		t.Fatalf("create u1: %v", err)
	}
	if err := st.CreateSelfHostedNodeForUser(2, mk(2, "u2-a", 443)); err != nil {
		t.Fatalf("create u2: %v", err)
	}

	// 同用户同身份 → 撞
	if dup, err := st.SelfHostedNodeIdentityExists(1, "dup.example.com", 443, "vless", 0); err != nil || !dup {
		t.Errorf("user1 同身份 dup=%v err=%v, want true", dup, err)
	}
	// 同用户不同端口 → 不撞
	if dup, err := st.SelfHostedNodeIdentityExists(1, "dup.example.com", 8443, "vless", 0); err != nil || dup {
		t.Errorf("user1 不同端口 dup=%v err=%v, want false", dup, err)
	}
	// 同用户同 server/port 不同协议 → 不撞(协议是身份三元组之一)
	if dup, err := st.SelfHostedNodeIdentityExists(1, "dup.example.com", 443, "trojan", 0); err != nil || dup {
		t.Errorf("user1 不同协议 dup=%v err=%v, want false", dup, err)
	}
	// 跨用户同身份 → 不撞(两个用户各自自建同一台服务器是合法场景)
	if dup, err := st.SelfHostedNodeIdentityExists(3, "dup.example.com", 443, "vless", 0); err != nil || dup {
		t.Errorf("user3 跨用户 dup=%v err=%v, want false", dup, err)
	}

	// 编辑排除自身:排除 u1 的行后,同身份不再命中(u2 的行不在 user1 域内)
	all, err := st.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(all) != 1 {
		t.Fatalf("list u1: n=%d err=%v", len(all), err)
	}
	if dup, err := st.SelfHostedNodeIdentityExists(1, "dup.example.com", 443, "vless", all[0].ID); err != nil || dup {
		t.Errorf("排除自身 dup=%v err=%v, want false", dup, err)
	}
}
