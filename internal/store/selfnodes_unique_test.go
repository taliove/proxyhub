package store

import (
	"errors"
	"testing"
)

// 自建节点身份唯一约束(issue #67):应用层查重挡不住 check-then-insert 竞态,
// DB 层 UNIQUE(user_id, server, port, protocol) 兜底;存量重复行按身份保留最早一行。
func TestSelfHostedIdentityUniqueMigration(t *testing.T) {
	st := newTestStore(t)

	// 模拟 #67 之前的环境:拆掉索引,手插两条同身份重复行(绕过应用层查重)
	if _, err := st.db.Exec(`DROP INDEX IF EXISTS idx_self_hosted_nodes_identity`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	insert := `INSERT INTO self_hosted_nodes (name, protocol, server, port, uuid, user_id)
	           VALUES (?, 'vless', 'dup.example.com', 443, '00000000-0000-0000-0000-000000000000', ?)`
	if _, err := st.db.Exec(insert, "最早", 1); err != nil {
		t.Fatalf("seed row1: %v", err)
	}
	if _, err := st.db.Exec(insert, "重复", 1); err != nil {
		t.Fatalf("seed row2: %v", err)
	}
	// 另一条不同身份行(不同用户),不应被清理
	if _, err := st.db.Exec(insert, "他人", 2); err != nil {
		t.Fatalf("seed row3: %v", err)
	}

	// 幂等迁移:清重 + 建索引
	if err := st.migrateSelfHostedIdentityUnique(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 二次执行幂等
	if err := st.migrateSelfHostedIdentityUnique(); err != nil {
		t.Fatalf("migrate 二次: %v", err)
	}

	rows, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2(重复行清理,跨用户行保留)", len(rows))
	}
	if rows[0].Name != "最早" {
		t.Errorf("保留行 name = %q, want 最早(保留 MIN(id))", rows[0].Name)
	}

	// 索引生效:直接 SQL 再插同身份行被拒
	if _, err := st.db.Exec(insert, "再重复", 1); err == nil {
		t.Error("唯一索引未生效:同身份行仍可插入")
	}
}

// 写入路径映射:CreateSelfHostedNodeForUser 撞唯一约束返回 ErrDuplicateIdentity。
func TestCreateSelfHostedNode_UniqueViolationMapped(t *testing.T) {
	st := newTestStore(t)

	mk := func() *SelfHostedNode {
		return &SelfHostedNode{
			Name: "A", Protocol: "vless", Server: "dup.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", UserID: 1, Enabled: true,
		}
	}
	if err := st.CreateSelfHostedNodeForUser(1, mk()); err != nil {
		t.Fatalf("首次创建: %v", err)
	}
	err := st.CreateSelfHostedNodeForUser(1, mk())
	if !errors.Is(err, ErrDuplicateIdentity) {
		t.Errorf("重复创建 err = %v, want ErrDuplicateIdentity", err)
	}
}

// 更新路径映射(check 评审 MEDIUM):UPDATE 撞唯一约束同样映射为 ErrDuplicateIdentity,
// 且 errors.Is 须穿透 fmt.Errorf %w 包装。
func TestUpdateSelfHostedNode_UniqueViolationMapped(t *testing.T) {
	st := newTestStore(t)

	mk := func(name string, port int) *SelfHostedNode {
		return &SelfHostedNode{
			Name: name, Protocol: "vless", Server: "dup.example.com", Port: port,
			UUID: "00000000-0000-0000-0000-000000000000", UserID: 1, Enabled: true,
		}
	}
	if err := st.CreateSelfHostedNodeForUser(1, mk("A", 443)); err != nil {
		t.Fatalf("创建 A: %v", err)
	}
	if err := st.CreateSelfHostedNodeForUser(1, mk("B", 8443)); err != nil {
		t.Fatalf("创建 B: %v", err)
	}

	// 把 B 改成 A 的身份 → ErrDuplicateIdentity
	rows, err := st.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(rows) != 2 {
		t.Fatalf("list: n=%d err=%v", len(rows), err)
	}
	b := rows[1]
	b.Server, b.Port, b.Protocol = "dup.example.com", 443, "vless"
	err = st.UpdateSelfHostedNodeForUser(1, b)
	if !errors.Is(err, ErrDuplicateIdentity) {
		t.Errorf("update 撞身份 err = %v, want ErrDuplicateIdentity", err)
	}
}
