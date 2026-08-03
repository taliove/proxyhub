package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openServerGeoTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestServerGeo_RoundTrip(t *testing.T) {
	st := openServerGeoTestStore(t)

	// 未写入:ok=false
	if _, _, ok := st.GetServerGeo("example.com"); ok {
		t.Fatal("GetServerGeo() ok = true for missing host, want false")
	}

	if err := st.PutServerGeo("example.com", "MV"); err != nil {
		t.Fatalf("PutServerGeo() error = %v", err)
	}
	code, resolvedAt, ok := st.GetServerGeo("example.com")
	if !ok {
		t.Fatal("GetServerGeo() ok = false after Put, want true")
	}
	if code != "MV" {
		t.Errorf("code = %q, want MV", code)
	}
	if time.Since(resolvedAt) > time.Minute {
		t.Errorf("resolved_at = %v, want fresh (within a minute)", resolvedAt)
	}

	// 负缓存:空 code 行同样可读
	if err := st.PutServerGeo("down.example.com", ""); err != nil {
		t.Fatalf("PutServerGeo() negative error = %v", err)
	}
	code, _, ok = st.GetServerGeo("down.example.com")
	if !ok || code != "" {
		t.Errorf("negative row = (%q, %v), want (\"\", true)", code, ok)
	}
}

func TestServerGeo_PutReplaces(t *testing.T) {
	st := openServerGeoTestStore(t)

	if err := st.PutServerGeo("example.com", ""); err != nil {
		t.Fatalf("PutServerGeo() error = %v", err)
	}
	// 负缓存被后来的正结果覆盖(INSERT OR REPLACE 幂等)
	if err := st.PutServerGeo("example.com", "BT"); err != nil {
		t.Fatalf("PutServerGeo() replace error = %v", err)
	}
	code, _, ok := st.GetServerGeo("example.com")
	if !ok || code != "BT" {
		t.Errorf("after replace = (%q, %v), want (BT, true)", code, ok)
	}
}

// 双路径迁移:既有库(建库后删表模拟旧库)再 Open 一次,表被幂等补建。
func TestServerGeo_MigrateIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := st.db.Exec(`DROP TABLE node_server_geo`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	st.Close()

	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("re-Open() error = %v (migration must recreate the table)", err)
	}
	defer st2.Close()
	if err := st2.PutServerGeo("example.com", "MV"); err != nil {
		t.Fatalf("PutServerGeo() after re-migration error = %v", err)
	}
}
