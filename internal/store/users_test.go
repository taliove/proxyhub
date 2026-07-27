package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateUser_Success(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("alice", "$2a$10$fakehashvalue", RoleUser, true)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if u.ID == 0 {
		t.Error("CreateUser() returned ID = 0")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want alice", u.Username)
	}
	if u.Role != RoleUser {
		t.Errorf("Role = %q, want %q", u.Role, RoleUser)
	}
	if !u.MustChangePassword {
		t.Error("MustChangePassword = false, want true")
	}
	if u.DisabledAt != nil {
		t.Errorf("DisabledAt = %v, want nil", u.DisabledAt)
	}
	if u.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("bob", "h1", RoleUser, false); err != nil {
		t.Fatalf("first CreateUser() error = %v", err)
	}
	_, err := s.CreateUser("bob", "h2", RoleUser, false)
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("second CreateUser() error = %v, want ErrUsernameTaken", err)
	}
}

func TestCreateUser_ReservedUsername(t *testing.T) {
	s := newTestStore(t)

	reserved := []string{"admin", "root", "ADMIN", "Root", " administrator ", "GUEST"}
	for _, name := range reserved {
		_, err := s.CreateUser(name, "h", RoleUser, false)
		if !errors.Is(err, ErrUsernameReserved) {
			t.Errorf("CreateUser(%q) error = %v, want ErrUsernameReserved", name, err)
		}
	}
}

func TestCreateUser_InvalidRole(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUser("carol", "h", "moderator", false)
	if !errors.Is(err, ErrInvalidRole) {
		t.Errorf("CreateUser() error = %v, want ErrInvalidRole", err)
	}
}

func TestCreateUser_EmptyFields(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateUser("", "h", RoleUser, false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty username: error = %v, want ErrInvalidInput", err)
	}
	if _, err := s.CreateUser("dave", "", RoleUser, false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty pass hash: error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateUser_TrimsWhitespace(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("  eve  ", "h", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if u.Username != "eve" {
		t.Errorf("Username = %q, want trimmed 'eve'", u.Username)
	}
}

func TestGetUserByUsername(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateUser("frank", "h", RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	got, err := s.GetUserByUsername("frank")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.Role != RoleSuperAdmin {
		t.Errorf("Role = %q, want %q", got.Role, RoleSuperAdmin)
	}

	_, err = s.GetUserByUsername("ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByUsername(ghost) error = %v, want ErrNotFound", err)
	}
}

func TestGetUserByID(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateUser("gina", "h", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	got, err := s.GetUserByID(created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if got.Username != "gina" {
		t.Errorf("Username = %q, want gina", got.Username)
	}

	_, err = s.GetUserByID(9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByID(9999) error = %v, want ErrNotFound", err)
	}
}

func TestUpdateUser_Role(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("henry", "h", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	newRole := RoleSuperAdmin
	if err := s.UpdateUser(u.ID, UserUpdate{Role: &newRole}); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	got, err := s.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if got.Role != RoleSuperAdmin {
		t.Errorf("Role = %q, want %q", got.Role, RoleSuperAdmin)
	}
}

func TestUpdateUser_PassHash(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("ivy", "old-hash", RoleUser, true)
	newHash := "new-hash"
	clearFlag := false
	if err := s.UpdateUser(u.ID, UserUpdate{
		PassHash:           &newHash,
		MustChangePassword: &clearFlag,
	}); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	got, _ := s.GetUserByID(u.ID)
	if got.PassHash != "new-hash" {
		t.Errorf("PassHash = %q, want new-hash", got.PassHash)
	}
	if got.MustChangePassword {
		t.Error("MustChangePassword = true, want false")
	}
}

func TestUpdateUser_LastLoginAt(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("jack", "h", RoleUser, false)
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateUser(u.ID, UserUpdate{LastLoginAt: &now}); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	got, _ := s.GetUserByID(u.ID)
	if got.LastLoginAt == nil {
		t.Fatal("LastLoginAt = nil, want non-nil")
	}
	if !got.LastLoginAt.Equal(now) {
		t.Errorf("LastLoginAt = %v, want %v", got.LastLoginAt, now)
	}
}

func TestUpdateUser_InvalidRole(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("kate", "h", RoleUser, false)
	bad := "admin-role"
	if err := s.UpdateUser(u.ID, UserUpdate{Role: &bad}); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("UpdateUser() error = %v, want ErrInvalidRole", err)
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	s := newTestStore(t)

	role := RoleUser
	if err := s.UpdateUser(9999, UserUpdate{Role: &role}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateUser() error = %v, want ErrNotFound", err)
	}
}

func TestUpdateUser_NoFields(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("leo", "h", RoleUser, false)
	if err := s.UpdateUser(u.ID, UserUpdate{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("UpdateUser() error = %v, want ErrInvalidInput", err)
	}
}

func TestDisableEnableUser(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("mia", "h", RoleUser, false)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.DisableUser(u.ID, now); err != nil {
		t.Fatalf("DisableUser() error = %v", err)
	}
	got, _ := s.GetUserByID(u.ID)
	if !got.Disabled() {
		t.Error("Disabled() = false, want true after DisableUser")
	}
	if got.DisabledAt == nil || !got.DisabledAt.Equal(now) {
		t.Errorf("DisabledAt = %v, want %v", got.DisabledAt, now)
	}

	if err := s.EnableUser(u.ID); err != nil {
		t.Fatalf("EnableUser() error = %v", err)
	}
	got, _ = s.GetUserByID(u.ID)
	if got.Disabled() {
		t.Error("Disabled() = true, want false after EnableUser")
	}
}

func TestDisableUser_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.DisableUser(9999, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("DisableUser() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteUser(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("nick", "h", RoleUser, false)
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}

	_, err := s.GetUserByID(u.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByID after delete error = %v, want ErrNotFound", err)
	}

	if err := s.DeleteUser(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteUser error = %v, want ErrNotFound", err)
	}
}

func TestListUsers(t *testing.T) {
	s := newTestStore(t)

	// MigrateAdminToSuperUser already ran on Open() with no admin_user set,
	// so the table starts empty.
	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 0 {
		t.Errorf("ListUsers() len = %d, want 0 on empty table", len(users))
	}

	for _, name := range []string{"u1", "u2", "u3"} {
		if _, err := s.CreateUser(name, "h", RoleUser, false); err != nil {
			t.Fatalf("CreateUser(%s) error = %v", name, err)
		}
	}

	users, err = s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 3 {
		t.Errorf("ListUsers() len = %d, want 3", len(users))
	}
	// Ordered by id: usernames should be in insertion order.
	wantOrder := []string{"u1", "u2", "u3"}
	for i, u := range users {
		if u.Username != wantOrder[i] {
			t.Errorf("users[%d].Username = %q, want %q", i, u.Username, wantOrder[i])
		}
	}
}

func TestListUsersWithQuotaUsage(t *testing.T) {
	s := newTestStore(t)

	u1, _ := s.CreateUser("owner1", "h", RoleUser, false)
	u2, _ := s.CreateUser("owner2", "h", RoleUser, false)

	if err := s.UpsertUserQuota(&UserQuota{
		UserID: u1.ID, MaxAirports: 5, MaxEndpoints: 10,
	}); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}

	out, err := s.ListUsersWithQuotaUsage()
	if err != nil {
		t.Fatalf("ListUsersWithQuotaUsage() error = %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("ListUsersWithQuotaUsage() len = %d, want 2", len(out))
	}

	var owner1Row *UserWithQuotaUsage
	for _, r := range out {
		if r.ID == u1.ID {
			owner1Row = r
		}
	}
	if owner1Row == nil {
		t.Fatal("owner1 row not found in ListUsersWithQuotaUsage()")
	}
	if owner1Row.Quota == nil {
		t.Fatal("owner1 Quota = nil, want non-nil")
	}
	if owner1Row.Quota.MaxAirports != 5 || owner1Row.Quota.MaxEndpoints != 10 {
		t.Errorf("Quota = %+v, want MaxAirports=5 MaxEndpoints=10", owner1Row.Quota)
	}

	// owner2 has no quota row: Quota must be nil.
	for _, r := range out {
		if r.ID == u2.ID && r.Quota != nil {
			t.Errorf("owner2 Quota = %+v, want nil", r.Quota)
		}
	}
}

func TestIsReservedUsername(t *testing.T) {
	cases := map[string]bool{
		"admin":     true,
		"ADMIN":     true,
		" Admin ":   true,
		"root":      true,
		"superuser": true,
		"alice":     false,
		"admin2":    false, // prefix match must not trigger
		"myroot":    false,
		"":          false,
	}
	for in, want := range cases {
		if got := IsReservedUsername(in); got != want {
			t.Errorf("IsReservedUsername(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCreateUser_UsernameTooLong(t *testing.T) {
	s := newTestStore(t)
	long := strings.Repeat("a", 65)
	if _, err := s.CreateUser(long, "h", RoleUser, false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("CreateUser(long) error = %v, want ErrInvalidInput", err)
	}
}
