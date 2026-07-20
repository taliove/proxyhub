package store

import (
	"path/filepath"
	"testing"

	"github.com/taliove/proxyhub/internal/jobs"
)

// TestMigration_JobsTableApplied 迁移执行测试:Open 后 jobs 表存在且 CRUD 可用。
func TestMigration_JobsTableApplied(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	// 表存在性(迁移已应用)。
	var name string
	err = st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='jobs'`).Scan(&name)
	if err != nil {
		t.Fatalf("jobs table not created: %v", err)
	}

	js := st.Jobs()
	if js == nil {
		t.Fatal("st.Jobs() is nil")
	}

	id, err := js.Insert("exam", "example.com:443", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Fatal("Insert returned id 0")
	}

	if err := js.UpdateCursor(id, "5"); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}
	if err := js.Finish(id, jobs.StatusDone); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	rec, err := js.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("Get returned nil for existing id")
	}
	if rec.Status != jobs.StatusDone {
		t.Errorf("status = %q, want done", rec.Status)
	}
	if rec.Cursor != "5" {
		t.Errorf("cursor = %q, want 5", rec.Cursor)
	}
	if rec.Kind != "exam" || rec.Key != "example.com:443" {
		t.Errorf("kind/key = %q/%q", rec.Kind, rec.Key)
	}
}

// TestMigration_Idempotent 重复 Open 同一文件不报错(迁移幂等)。
func TestMigration_JobsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	id, err := st1.Jobs().Insert("exam", "k", nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	st1.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("Open 2 (re-migrate): %v", err)
	}
	defer st2.Close()

	rec, err := st2.Jobs().Get(id)
	if err != nil || rec == nil {
		t.Fatalf("row lost across re-open: rec=%v err=%v", rec, err)
	}
}
