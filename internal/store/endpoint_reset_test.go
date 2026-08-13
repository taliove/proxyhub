package store

import (
	"testing"
	"time"
)

// issue #117:订阅链接重置——原位轮换 path+token,旧对进 prev 槽位,
// 默认 3 天宽限,可延长(+3 天/次,仅存活期),再次重置覆盖,过期不可复活。

func TestResetEndpointLink_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ep, err := s.CreateEndpointForUser(1, "重置测试")
	if err != nil {
		t.Fatalf("CreateEndpointForUser() error = %v", err)
	}
	oldPath, oldToken := ep.Path, ep.Token

	got, err := s.ResetEndpointLinkForUser(1, ep.ID)
	if err != nil {
		t.Fatalf("ResetEndpointLinkForUser() error = %v", err)
	}
	if got.Path == oldPath || got.Token == oldToken {
		t.Errorf("path/token 未轮换: %+v", got)
	}
	if got.PrevPath != oldPath || got.PrevToken != oldToken {
		t.Errorf("prev 槽位 = %q/%q, want 旧对", got.PrevPath, got.PrevToken)
	}
	expiry, err := time.ParseInLocation(graceTimeLayout, got.GraceExpiresAt, time.UTC)
	if err != nil {
		t.Fatalf("grace_expires_at 解析失败: %q", got.GraceExpiresAt)
	}
	delta := time.Until(expiry)
	if delta < 71*time.Hour || delta > 73*time.Hour {
		t.Errorf("grace delta = %v, want ~72h", delta)
	}

	// prev 反查可用
	byPrev, err := s.GetEndpointByPrevPath(oldPath)
	if err != nil || byPrev.ID != ep.ID {
		t.Errorf("GetEndpointByPrevPath() = %v, %v", byPrev, err)
	}
	// 宽限存活判定
	if !byPrev.GraceAlive(time.Now().UTC()) {
		t.Error("GraceAlive(now) = false, want true(刚重置)")
	}
}

func TestResetEndpointLink_ReResetOverwritesPrev(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpointForUser(1, "二次重置")
	first, err := s.ResetEndpointLinkForUser(1, ep.ID)
	if err != nil {
		t.Fatalf("first reset error = %v", err)
	}
	second, err := s.ResetEndpointLinkForUser(1, ep.ID)
	if err != nil {
		t.Fatalf("second reset error = %v", err)
	}
	if second.PrevPath != first.Path {
		t.Errorf("prev after re-reset = %q, want 第一代新 path %q(只保留一代)", second.PrevPath, first.Path)
	}
	// 第一代的旧 path 不再可反查
	if _, err := s.GetEndpointByPrevPath(ep.Path); err == nil {
		t.Error("原始 path 在二次重置后仍可 prev 反查, want ErrNotFound")
	}
}

func TestExtendEndpointGrace(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpointForUser(1, "延长宽限")
	reset, err := s.ResetEndpointLinkForUser(1, ep.ID)
	if err != nil {
		t.Fatalf("reset error = %v", err)
	}
	before, _ := time.ParseInLocation(graceTimeLayout, reset.GraceExpiresAt, time.UTC)

	got, err := s.ExtendEndpointGraceForUser(1, ep.ID)
	if err != nil {
		t.Fatalf("ExtendEndpointGraceForUser() error = %v", err)
	}
	after, _ := time.ParseInLocation(graceTimeLayout, got.GraceExpiresAt, time.UTC)
	if after.Sub(before) != 72*time.Hour {
		t.Errorf("extend delta = %v, want 72h", after.Sub(before))
	}

	// 从未重置的端点没有宽限可延:ErrNotFound(过期/不存在不可区分)
	plain, _ := s.CreateEndpointForUser(1, "未重置")
	if _, err := s.ExtendEndpointGraceForUser(1, plain.ID); err != ErrNotFound {
		t.Errorf("extend never-reset = %v, want ErrNotFound", err)
	}
	// 属主隔离
	if _, err := s.ExtendEndpointGraceForUser(2, ep.ID); err != ErrNotFound {
		t.Errorf("extend other user's endpoint = %v, want ErrNotFound", err)
	}
	if _, err := s.ResetEndpointLinkForUser(2, ep.ID); err != ErrNotFound {
		t.Errorf("reset other user's endpoint = %v, want ErrNotFound", err)
	}
}

// 宽限过期:GraceAlive 为 false(订阅校验链据此 404),且不可复活(延长 ErrNotFound)。
func TestEndpointGrace_ExpiredNotRevivable(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpointForUser(1, "过期宽限")
	if _, err := s.ResetEndpointLinkForUser(1, ep.ID); err != nil {
		t.Fatalf("reset error = %v", err)
	}
	if _, err := s.db.Exec(
		`UPDATE endpoints SET grace_expires_at = '2000-01-01 00:00:00' WHERE id = ?`, ep.ID); err != nil {
		t.Fatalf("backdate grace: %v", err)
	}

	got, err := s.GetEndpointByPrevPath(ep.Path)
	if err != nil {
		t.Fatalf("GetEndpointByPrevPath() error = %v", err)
	}
	if got.GraceAlive(time.Now().UTC()) {
		t.Error("GraceAlive(expired) = true, want false")
	}
	if _, err := s.ExtendEndpointGraceForUser(1, ep.ID); err != ErrNotFound {
		t.Errorf("extend expired grace = %v, want ErrNotFound(不可复活)", err)
	}
}
