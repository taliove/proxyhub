package store

import (
	"errors"
	"testing"
)

func TestUpsertAndGetUserQuota(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("quota-owner", "h", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	// Missing row -> ErrNotFound.
	if _, err := s.GetUserQuota(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserQuota on empty error = %v, want ErrNotFound", err)
	}

	want := &UserQuota{
		UserID:        u.ID,
		MaxAirports:   3,
		MaxEndpoints:  7,
		XrayPortStart: 20000,
		XrayPortEnd:   20099,
	}
	if err := s.UpsertUserQuota(want); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}

	got, err := s.GetUserQuota(u.ID)
	if err != nil {
		t.Fatalf("GetUserQuota() error = %v", err)
	}
	if *got != *want {
		t.Errorf("GetUserQuota = %+v, want %+v", got, want)
	}

	// Upsert again with new values -> replaced, not duplicated.
	updated := &UserQuota{UserID: u.ID, MaxAirports: 10, MaxEndpoints: 20}
	if err := s.UpsertUserQuota(updated); err != nil {
		t.Fatalf("second UpsertUserQuota() error = %v", err)
	}
	got, err = s.GetUserQuota(u.ID)
	if err != nil {
		t.Fatalf("GetUserQuota() error = %v", err)
	}
	if got.MaxAirports != 10 || got.MaxEndpoints != 20 {
		t.Errorf("after upsert got %+v, want MaxAirports=10 MaxEndpoints=20", got)
	}
	if got.XrayPortStart != 0 || got.XrayPortEnd != 0 {
		t.Errorf("after upsert port range = %d-%d, want reset to 0-0",
			got.XrayPortStart, got.XrayPortEnd)
	}
}

func TestUpsertUserQuota_Validation(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("validator", "h", RoleUser, false)

	cases := []struct {
		name  string
		quota *UserQuota
	}{
		{"nil", nil},
		{"zero user id", &UserQuota{UserID: 0}},
		{"negative airports", &UserQuota{UserID: u.ID, MaxAirports: -1}},
		{"negative endpoints", &UserQuota{UserID: u.ID, MaxEndpoints: -1}},
		{"negative port start", &UserQuota{UserID: u.ID, XrayPortStart: -1}},
		{"negative port end", &UserQuota{UserID: u.ID, XrayPortEnd: -1}},
		{"inverted range", &UserQuota{UserID: u.ID, XrayPortStart: 20100, XrayPortEnd: 20000}},
	}
	for _, tc := range cases {
		if err := s.UpsertUserQuota(tc.quota); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

func TestDeleteUserQuota(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("deleteme", "h", RoleUser, false)

	if err := s.DeleteUserQuota(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUserQuota on missing error = %v, want ErrNotFound", err)
	}

	if err := s.UpsertUserQuota(&UserQuota{UserID: u.ID, MaxAirports: 1}); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}
	if err := s.DeleteUserQuota(u.ID); err != nil {
		t.Fatalf("DeleteUserQuota() error = %v", err)
	}
	if _, err := s.GetUserQuota(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserQuota after delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteUser_CascadesQuota(t *testing.T) {
	s := newTestStore(t)

	// SQLite requires PRAGMA foreign_keys = ON per connection to enforce
	// ON DELETE CASCADE. modernc.org/sqlite defaults to off, so enable it.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	u, _ := s.CreateUser("cascade-test", "h", RoleUser, false)
	if err := s.UpsertUserQuota(&UserQuota{UserID: u.ID, MaxAirports: 1}); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if _, err := s.GetUserQuota(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserQuota after user delete error = %v, want ErrNotFound", err)
	}
}
