package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateEndpointPublicName_SchemaInPlace a freshly opened database carries
// the public_name column.
func TestMigrateEndpointPublicName_SchemaInPlace(t *testing.T) {
	s := newTestStore(t)
	if !s.columnExistsUnlocked("endpoints", "public_name") {
		t.Error("endpoints.public_name column missing after migrate()")
	}
}

// TestMigrateEndpointPublicName_UpgradesLegacyTable is the existing-database
// path: a table created without public_name gains it, and rows written before
// the upgrade read back as empty rather than NULL.
func TestMigrateEndpointPublicName_UpgradesLegacyTable(t *testing.T) {
	s := newTestStore(t)

	// Rebuild endpoints in the pre-issue-38 shape, with one legacy row in it.
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS endpoints_legacy;
CREATE TABLE endpoints_legacy (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	alias         TEXT NOT NULL,
	path          TEXT NOT NULL UNIQUE,
	token         TEXT NOT NULL,
	enabled       INTEGER NOT NULL DEFAULT 1,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	name_mode     TEXT NOT NULL DEFAULT '',
	name_template TEXT NOT NULL DEFAULT '',
	conditions    TEXT NOT NULL DEFAULT '',
	user_id       INTEGER NOT NULL DEFAULT 0,
	template_name TEXT NOT NULL DEFAULT '',
	geo_mode      TEXT NOT NULL DEFAULT 'off',
	geo_countries TEXT NOT NULL DEFAULT '',
	geo_provinces TEXT NOT NULL DEFAULT ''
);
DROP TABLE endpoints;
ALTER TABLE endpoints_legacy RENAME TO endpoints;
INSERT INTO endpoints (alias, path, token) VALUES ('example.com', 'legacypath000000', 'legacytoken');`); err != nil {
		t.Fatalf("stage legacy endpoints table: %v", err)
	}

	if err := s.migrateEndpointPublicName(); err != nil {
		t.Fatalf("migrateEndpointPublicName: %v", err)
	}
	// A real upgrade runs the whole migrate chain; the read path now also
	// selects node_picks (issue #79), so the staged legacy table must gain it
	// the same way production would (same pattern as the geo legacy test).
	if err := s.migrateEndpointNodePicks(); err != nil {
		t.Fatalf("migrateEndpointNodePicks: %v", err)
	}
	// status_node_enabled(issue #102)同理:读路径已选该列,legacy 表须同步补
	if err := s.migrateEndpointStatusNode(); err != nil {
		t.Fatalf("migrateEndpointStatusNode: %v", err)
	}

	ep, err := s.GetEndpointByPath("legacypath000000")
	if err != nil {
		t.Fatalf("GetEndpointByPath after upgrade: %v", err)
	}
	if ep.PublicName != "" {
		t.Errorf("legacy row PublicName = %q, want empty", ep.PublicName)
	}
}

// TestMigrateEndpointPublicName_Idempotent running the migration again (and
// after a reopen) neither fails nor clobbers a configured public name.
func TestMigrateEndpointPublicName_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ep, err := s.CreateEndpoint("example.com")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if err := s.UpdateEndpointPublicName(ep.ID, "home broadband"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}
	if err := s.migrateEndpointPublicName(); err != nil {
		t.Fatalf("migrateEndpointPublicName (second run): %v", err)
	}
	s.Close()

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	got, err := reopened.GetEndpointByID(ep.ID)
	if err != nil {
		t.Fatalf("GetEndpointByID: %v", err)
	}
	if got.PublicName != "home broadband" {
		t.Errorf("PublicName after reopen = %q, want home broadband", got.PublicName)
	}
}

// TestEndpointPublicNameDefaults a fresh address has no public name: the /sub
// handler then falls back to the bare brand title.
func TestEndpointPublicNameDefaults(t *testing.T) {
	s := newTestStore(t)
	ep, err := s.CreateEndpoint("example.com")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if ep.PublicName != "" {
		t.Errorf("PublicName = %q, want empty", ep.PublicName)
	}
}

// TestUpdateEndpointPublicName_Roundtrip exercises every read path that shares
// endpointColumns/scanEndpointFrom: GetEndpointByID, GetEndpointByPath and
// ListEndpoints all must carry the new column.
func TestUpdateEndpointPublicName_Roundtrip(t *testing.T) {
	s := newTestStore(t)
	ep, err := s.CreateEndpoint("example.com")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	if err := s.UpdateEndpointPublicName(ep.ID, "家里宽带"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}

	byID, err := s.GetEndpointByID(ep.ID)
	if err != nil {
		t.Fatalf("GetEndpointByID: %v", err)
	}
	if byID.PublicName != "家里宽带" {
		t.Errorf("GetEndpointByID PublicName = %q, want 家里宽带", byID.PublicName)
	}

	byPath, err := s.GetEndpointByPath(ep.Path)
	if err != nil {
		t.Fatalf("GetEndpointByPath: %v", err)
	}
	if byPath.PublicName != "家里宽带" {
		t.Errorf("GetEndpointByPath PublicName = %q, want 家里宽带", byPath.PublicName)
	}

	list, err := s.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(list) != 1 || list[0].PublicName != "家里宽带" {
		t.Fatalf("ListEndpoints PublicName mismatch: %+v", list)
	}

	// Empty string clears the name.
	if err := s.UpdateEndpointPublicName(ep.ID, ""); err != nil {
		t.Fatalf("clear PublicName: %v", err)
	}
	cleared, _ := s.GetEndpointByID(ep.ID)
	if cleared.PublicName != "" {
		t.Errorf("PublicName after clear = %q, want empty", cleared.PublicName)
	}
}

// TestUpdateEndpointPublicName_Sanitises the store boundary trims surrounding
// whitespace, strips control characters and truncates at 50 runes.
func TestUpdateEndpointPublicName_Sanitises(t *testing.T) {
	s := newTestStore(t)
	ep, err := s.CreateEndpoint("example.com")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trim", "  office line  ", "office line"},
		{"control chars stripped", "a\r\nb\tc\x00d", "abcd"},
		{"whitespace only clears", "   ", ""},
		{"rune cap", strings.Repeat("名", 60), strings.Repeat("名", 50)},
		{"rune cap boundary", strings.Repeat("a", 50), strings.Repeat("a", 50)},
	}
	for _, c := range cases {
		if err := s.UpdateEndpointPublicName(ep.ID, c.in); err != nil {
			t.Fatalf("%s: UpdateEndpointPublicName: %v", c.name, err)
		}
		got, _ := s.GetEndpointByID(ep.ID)
		if got.PublicName != c.want {
			t.Errorf("%s: PublicName = %q, want %q", c.name, got.PublicName, c.want)
		}
	}
}

// TestUpdateEndpointPublicNameForUser_OwnershipScoped a foreign row answers
// ErrNotFound and keeps its name, same rule as the other per-owner endpoint
// writers; userID=0 skips the owner check (global escape hatch).
func TestUpdateEndpointPublicNameForUser_OwnershipScoped(t *testing.T) {
	s := newTestStore(t)
	owner, err := s.CreateUser("pn-owner", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := s.CreateUser("pn-other", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ep, err := s.CreateEndpointForUser(owner.ID, "example.com")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	err = s.UpdateEndpointPublicNameForUser(other.ID, ep.ID, "foreign write")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update err = %v, want ErrNotFound", err)
	}
	got, _ := s.GetEndpointByID(ep.ID)
	if got.PublicName != "" {
		t.Errorf("PublicName = %q, want empty (foreign write must not land)", got.PublicName)
	}

	if err := s.UpdateEndpointPublicNameForUser(owner.ID, ep.ID, "owner name"); err != nil {
		t.Fatalf("owner update: %v", err)
	}
	got, _ = s.GetEndpointByID(ep.ID)
	if got.PublicName != "owner name" {
		t.Errorf("owner update did not land: %q", got.PublicName)
	}

	// userID=0 escape hatch bypasses the owner check.
	if err := s.UpdateEndpointPublicNameForUser(0, ep.ID, "global write"); err != nil {
		t.Fatalf("global update: %v", err)
	}
	got, _ = s.GetEndpointByID(ep.ID)
	if got.PublicName != "global write" {
		t.Errorf("global update did not land: %q", got.PublicName)
	}
}
