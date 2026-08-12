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

// TestStore_GetLatestJobByKindKey tests retrieval of the most recent job by kind and key.
func TestStore_GetLatestJobByKindKey(t *testing.T) {
	st := newTestStore(t)

	// No job exists
	rec, err := st.GetLatestJobByKindKey("retag_all", "nightly")
	if err != nil {
		t.Fatalf("GetLatestJobByKindKey on empty: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil for non-existent job, got %+v", rec)
	}

	// Insert multiple jobs with same kind/key
	js := st.Jobs()
	id1, err := js.Insert("retag_all", "nightly", nil)
	if err != nil {
		t.Fatalf("Insert 1: %v", err)
	}
	if err := js.Finish(id1, jobs.StatusDone); err != nil {
		t.Fatalf("Finish 1: %v", err)
	}

	id2, err := js.Insert("retag_all", "nightly", nil)
	if err != nil {
		t.Fatalf("Insert 2: %v", err)
	}
	if err := js.Finish(id2, jobs.StatusDone); err != nil {
		t.Fatalf("Finish 2: %v", err)
	}

	// Should return the latest (id2)
	rec, err = st.GetLatestJobByKindKey("retag_all", "nightly")
	if err != nil {
		t.Fatalf("GetLatestJobByKindKey: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.ID != id2 {
		t.Errorf("expected latest job id %d, got %d", id2, rec.ID)
	}

	// Different key should not match
	rec, err = st.GetLatestJobByKindKey("retag_all", "other")
	if err != nil {
		t.Fatalf("GetLatestJobByKindKey with different key: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil for different key, got %+v", rec)
	}
}

// TestStore_GetLatestJobByKindKeyForUser 按属主过滤:他人同 kind/key 任务不可见
// (夜间全员补齐按用户去重的查询契约,pre-push 评审 MEDIUM)。
func TestStore_GetLatestJobByKindKeyForUser(t *testing.T) {
	st := newTestStore(t)
	js := st.Jobs()

	// user 1 与用户 2 各跑一个 batch_exam;另有一条未归属(0)
	idU1, err := js.InsertForUser(1, "batch_exam", "batch_exam", nil)
	if err != nil {
		t.Fatalf("InsertFor u1: %v", err)
	}
	if _, err := js.InsertForUser(2, "batch_exam", "batch_exam", nil); err != nil {
		t.Fatalf("InsertFor u2: %v", err)
	}

	rec, err := st.GetLatestJobByKindKeyForUser(1, "batch_exam", "batch_exam")
	if err != nil {
		t.Fatalf("GetLatestJobByKindKeyForUser: %v", err)
	}
	if rec == nil || rec.ID != idU1 {
		t.Errorf("user 1 latest = %+v, want id %d", rec, idU1)
	}

	// 无任务的用户拿到 nil(不是他人的)
	rec0, err := st.GetLatestJobByKindKeyForUser(99, "batch_exam", "batch_exam")
	if err != nil || rec0 != nil {
		t.Errorf("user 99 latest = %+v err=%v, want nil", rec0, err)
	}
}
