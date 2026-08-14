package store

import (
	"errors"
	"testing"
)

func speedtestFixture(nodeKey string, down float64) SpeedtestResult {
	return SpeedtestResult{
		NodeKey:       nodeKey,
		DownMbps:      down,
		UpMbps:        down / 2,
		IdleLatencyMs: 12.5,
		JitterMs:      1.5,
		ClientInfo:    "test-agent",
	}
}

// TestSpeedtestResults_SaveAndListAll 落库后全量列出,时间倒序(id DESC),字段完整。
func TestSpeedtestResults_SaveAndListAll(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.SaveSpeedtestResult(speedtestFixture("", 100)); err != nil {
		t.Fatalf("save direct: %v", err)
	}
	if _, err := st.SaveSpeedtestResult(speedtestFixture("1.2.3.4:8388", 50)); err != nil {
		t.Fatalf("save keyed: %v", err)
	}

	list, err := st.ListSpeedtestResults(nil)
	if err != nil {
		t.Fatalf("ListSpeedtestResults: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	// id DESC:后写(keyed)在前
	if list[0].NodeKey != "1.2.3.4:8388" {
		t.Errorf("list[0].NodeKey = %q, want keyed entry first (id DESC)", list[0].NodeKey)
	}
	// 直连条目:NULL 读回为空串
	if list[1].NodeKey != "" {
		t.Errorf("list[1].NodeKey = %q, want empty (direct)", list[1].NodeKey)
	}
	if list[0].DownMbps != 50 || list[0].UpMbps != 25 || list[0].IdleLatencyMs != 12.5 ||
		list[0].JitterMs != 1.5 || list[0].ClientInfo != "test-agent" {
		t.Errorf("fields round-trip mismatch: %+v", list[0])
	}
	if list[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want parsed timestamp")
	}
	if list[0].ID == 0 {
		t.Error("ID is zero, want assigned autoincrement id")
	}
}

// TestSpeedtestResults_ListFilter node_key 过滤:直连(空)与具体 key 各自成桶。
func TestSpeedtestResults_ListFilter(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.SaveSpeedtestResult(speedtestFixture("", 100)); err != nil {
		t.Fatalf("save direct: %v", err)
	}
	if _, err := st.SaveSpeedtestResult(speedtestFixture("a:1", 50)); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if _, err := st.SaveSpeedtestResult(speedtestFixture("b:2", 30)); err != nil {
		t.Fatalf("save b: %v", err)
	}

	direct := ""
	list, err := st.ListSpeedtestResults(&direct)
	if err != nil {
		t.Fatalf("list direct: %v", err)
	}
	if len(list) != 1 || list[0].NodeKey != "" {
		t.Errorf("direct filter = %+v, want only the direct entry", list)
	}

	keyA := "a:1"
	list, err = st.ListSpeedtestResults(&keyA)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(list) != 1 || list[0].NodeKey != "a:1" {
		t.Errorf("key filter = %+v, want only a:1", list)
	}

	// 孤儿历史:节点已不在池(无此 key)不影响读出
	orphan := "gone:9"
	list, err = st.ListSpeedtestResults(&orphan)
	if err != nil {
		t.Fatalf("list orphan: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("orphan filter len = %d, want 0", len(list))
	}
}

// TestSpeedtestResults_PrunePerKey 每 key(含直连桶)保留最近 50 条,写入即修剪;
// 各桶互不影响。
func TestSpeedtestResults_PrunePerKey(t *testing.T) {
	st := newTestStore(t)

	for i := 0; i < speedtestRetention+2; i++ {
		if _, err := st.SaveSpeedtestResult(speedtestFixture("a:1", float64(i))); err != nil {
			t.Fatalf("save a #%d: %v", i, err)
		}
	}
	for i := 0; i < speedtestRetention+1; i++ {
		if _, err := st.SaveSpeedtestResult(speedtestFixture("", float64(i))); err != nil {
			t.Fatalf("save direct #%d: %v", i, err)
		}
	}

	keyA := "a:1"
	listA, err := st.ListSpeedtestResults(&keyA)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(listA) != speedtestRetention {
		t.Errorf("a bucket len = %d, want %d after prune", len(listA), speedtestRetention)
	}
	// 最新保留、最旧修剪:a 写了 0..51,应保留 2..51
	if listA[len(listA)-1].DownMbps != 2 {
		t.Errorf("oldest kept down = %v, want 2 (0/1 pruned)", listA[len(listA)-1].DownMbps)
	}

	direct := ""
	listD, err := st.ListSpeedtestResults(&direct)
	if err != nil {
		t.Fatalf("list direct: %v", err)
	}
	if len(listD) != speedtestRetention {
		t.Errorf("direct bucket len = %d, want %d after prune", len(listD), speedtestRetention)
	}
	// 直连桶修剪不波及 a 桶(已在上面断言),反向:a 的 52 条不撑爆直连桶
}

// TestSpeedtestResults_Delete 删除存在的条目成功;删不存在返回 ErrNotFound。
func TestSpeedtestResults_Delete(t *testing.T) {
	st := newTestStore(t)

	id, err := st.SaveSpeedtestResult(speedtestFixture("", 100))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("save returned id 0")
	}

	if err := st.DeleteSpeedtestResult(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err := st.ListSpeedtestResults(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list len = %d after delete, want 0", len(list))
	}

	if err := st.DeleteSpeedtestResult(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: err = %v, want ErrNotFound", err)
	}
}

// TestSpeedtestResults_MigrationIdempotent 迁移幂等:重复 Open 不报错,数据保留。
func TestSpeedtestResults_MigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenForTesting(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.SaveSpeedtestResult(speedtestFixture("", 88)); err != nil {
		t.Fatalf("save: %v", err)
	}
	st.Close()

	st2, err := OpenForTesting(dir + "/test.db")
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer st2.Close()
	list, err := st2.ListSpeedtestResults(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].DownMbps != 88 {
		t.Errorf("after re-Open list = %+v, want the saved entry", list)
	}
}
