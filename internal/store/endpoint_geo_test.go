package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestMigrateEndpointGeo_SchemaInPlace a freshly opened database carries the
// three geo columns.
func TestMigrateEndpointGeo_SchemaInPlace(t *testing.T) {
	s := newTestStore(t)
	for _, col := range []string{"geo_mode", "geo_countries", "geo_provinces"} {
		if !s.columnExistsUnlocked("endpoints", col) {
			t.Errorf("endpoints.%s column missing after migrate()", col)
		}
	}
}

// TestMigrateEndpointGeo_UpgradesLegacyTable is the existing-database path: a
// table created without the geo columns gains them, and rows written before the
// upgrade read back as off / empty rather than NULL.
func TestMigrateEndpointGeo_UpgradesLegacyTable(t *testing.T) {
	s := newTestStore(t)

	// Rebuild endpoints in the pre-ticket-07 shape, with one legacy row in it.
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
	template_name TEXT NOT NULL DEFAULT ''
);
DROP TABLE endpoints;
ALTER TABLE endpoints_legacy RENAME TO endpoints;
INSERT INTO endpoints (alias, path, token) VALUES ('存量设备', 'legacypath000000', 'legacytoken');`); err != nil {
		t.Fatalf("stage legacy endpoints table: %v", err)
	}

	if err := s.migrateEndpointGeo(); err != nil {
		t.Fatalf("migrateEndpointGeo: %v", err)
	}
	// A real upgrade runs the whole migrate chain; the read path now also
	// selects public_name (issue #38) and node_picks (issue #79), so the staged
	// legacy table must gain them the same way production would.
	if err := s.migrateEndpointPublicName(); err != nil {
		t.Fatalf("migrateEndpointPublicName: %v", err)
	}
	if err := s.migrateEndpointNodePicks(); err != nil {
		t.Fatalf("migrateEndpointNodePicks: %v", err)
	}
	// status_node_enabled(issue #102)同理:读路径已选该列,legacy 表须同步补
	if err := s.migrateEndpointStatusNode(); err != nil {
		t.Fatalf("migrateEndpointStatusNode: %v", err)
	}
	// slot_mode 同理
	if err := s.migrateEndpointSlotMode(); err != nil {
		t.Fatalf("migrateEndpointSlotMode: %v", err)
	}

	ep, err := s.GetEndpointByPath("legacypath000000")
	if err != nil {
		t.Fatalf("GetEndpointByPath after upgrade: %v", err)
	}
	if ep.GeoMode != GeoModeOff {
		t.Errorf("legacy row GeoMode = %q, want off", ep.GeoMode)
	}
	if ep.GeoCountries != "" || ep.GeoProvinces != "" {
		t.Errorf("legacy row lists = %q/%q, want both empty", ep.GeoCountries, ep.GeoProvinces)
	}
}

// TestMigrateEndpointGeo_Idempotent running the migration again (and after a
// reopen) neither fails nor clobbers a configured allowlist.
func TestMigrateEndpointGeo_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ep, err := s.CreateEndpoint("幂等设备")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if err := s.UpdateEndpointGeoConfig(ep.ID, GeoModeEnforce, "cn", "GD"); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}
	if err := s.migrateEndpointGeo(); err != nil {
		t.Fatalf("migrateEndpointGeo (second run): %v", err)
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
	if got.GeoMode != GeoModeEnforce || got.GeoCountries != "CN" || got.GeoProvinces != "GD" {
		t.Errorf("geo config after reopen = %q/%q/%q, want enforce/CN/GD",
			got.GeoMode, got.GeoCountries, got.GeoProvinces)
	}
}

// TestEndpointGeoDefaults a fresh address is inert: mode off, both lists empty.
// This is the compatibility guarantee for every pre-existing subscription.
func TestEndpointGeoDefaults(t *testing.T) {
	s := newTestStore(t)
	ep, err := s.CreateEndpoint("默认设备")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if ep.GeoMode != GeoModeOff {
		t.Errorf("GeoMode = %q, want off", ep.GeoMode)
	}
	if ep.GeoCountries != "" || ep.GeoProvinces != "" {
		t.Errorf("lists = %q/%q, want both empty", ep.GeoCountries, ep.GeoProvinces)
	}
}

// TestUpdateEndpointGeoConfig_Normalises country codes are upper-cased and
// deduplicated, provinces keep their spelling, blank entries disappear.
func TestUpdateEndpointGeoConfig_Normalises(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("归一化设备")

	if err := s.UpdateEndpointGeoConfig(ep.ID, GeoModeObserve, " cn , jp ,CN,", "Guangdong, ,GD"); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}
	got, _ := s.GetEndpointByID(ep.ID)
	if got.GeoCountries != "CN,JP" {
		t.Errorf("GeoCountries = %q, want CN,JP", got.GeoCountries)
	}
	if got.GeoProvinces != "Guangdong,GD" {
		t.Errorf("GeoProvinces = %q, want Guangdong,GD", got.GeoProvinces)
	}
	if got.GeoMode != GeoModeObserve {
		t.Errorf("GeoMode = %q, want observe", got.GeoMode)
	}
}

// TestUpdateEndpointGeoConfig_EmptyModeIsOff an omitted mode stores off rather
// than an empty string no reader knows how to interpret.
func TestUpdateEndpointGeoConfig_EmptyModeIsOff(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("空模式设备")

	if err := s.UpdateEndpointGeoConfig(ep.ID, "", "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}
	got, _ := s.GetEndpointByID(ep.ID)
	if got.GeoMode != GeoModeOff {
		t.Errorf("GeoMode = %q, want off", got.GeoMode)
	}
}

// TestUpdateEndpointGeoConfig_RejectsUnknownMode a typo must not reach the
// column: the guard would read it as "not off" and start judging.
func TestUpdateEndpointGeoConfig_RejectsUnknownMode(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("非法模式设备")

	if err := s.UpdateEndpointGeoConfig(ep.ID, "enfroce", "CN", ""); err == nil {
		t.Fatal("expected an error for an unknown geo_mode, got nil")
	}
	got, _ := s.GetEndpointByID(ep.ID)
	if got.GeoMode != GeoModeOff {
		t.Errorf("GeoMode = %q, want the row untouched (off)", got.GeoMode)
	}
}

// TestUpdateEndpointGeoConfigForUser_OwnershipScoped a foreign row answers
// ErrNotFound and keeps its configuration, same rule as the other per-owner
// endpoint writers.
func TestUpdateEndpointGeoConfigForUser_OwnershipScoped(t *testing.T) {
	s := newTestStore(t)
	owner, err := s.CreateUser("geo-owner", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := s.CreateUser("geo-other", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ep, err := s.CreateEndpointForUser(owner.ID, "属主设备")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	err = s.UpdateEndpointGeoConfigForUser(other.ID, ep.ID, GeoModeEnforce, "US", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update err = %v, want ErrNotFound", err)
	}
	got, _ := s.GetEndpointByID(ep.ID)
	if got.GeoMode != GeoModeOff {
		t.Errorf("GeoMode = %q, want off (foreign write must not land)", got.GeoMode)
	}

	if err := s.UpdateEndpointGeoConfigForUser(owner.ID, ep.ID, GeoModeEnforce, "US", ""); err != nil {
		t.Fatalf("owner update: %v", err)
	}
	got, _ = s.GetEndpointByID(ep.ID)
	if got.GeoMode != GeoModeEnforce || got.GeoCountries != "US" {
		t.Errorf("owner update did not land: %q/%q", got.GeoMode, got.GeoCountries)
	}
}

// TestParseGeoList blank and space-padded entries never become allowlist
// members: a trailing comma must not turn into an entry that matches nothing
// (or, worse, an empty-string comparison).
func TestParseGeoList(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil},
		{"CN", []string{"CN"}},
		{" CN , JP ,", []string{"CN", "JP"}},
	}
	for _, c := range cases {
		got := ParseGeoList(c.raw)
		if len(got) != len(c.want) {
			t.Errorf("ParseGeoList(%q) = %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseGeoList(%q)[%d] = %q, want %q", c.raw, i, got[i], c.want[i])
			}
		}
	}
}
