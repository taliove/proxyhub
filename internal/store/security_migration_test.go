package store

import (
	"database/sql"
	"testing"
	"time"
)

// insertLegacyBannedIP writes a banned_ips row the pre-fix way: binding
// time.Time directly, which the modernc driver serialises with
// time.Time.String() (monotonic clock suffix included). SQLite's datetime()
// cannot parse that shape.
func insertLegacyBannedIP(t *testing.T, s *Store, ip string, bannedUntil, updatedAt time.Time) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO banned_ips (ip, fail_count, banned_until, updated_at) VALUES (?, 0, ?, ?)`,
		ip, bannedUntil, updatedAt,
	); err != nil {
		t.Fatalf("insert legacy row %s: %v", ip, err)
	}
	// Guard the premise: the row must really be unreadable by datetime().
	var parsed sql.NullString
	if err := s.db.QueryRow(
		`SELECT datetime(banned_until) FROM banned_ips WHERE ip = ?`, ip,
	).Scan(&parsed); err != nil {
		t.Fatalf("probe legacy row %s: %v", ip, err)
	}
	if parsed.Valid {
		t.Fatalf("legacy row %s: datetime(banned_until) = %q, want NULL (test premise broken)", ip, parsed.String)
	}
}

// rawColumn reads a banned_ips time column as raw text, bypassing the driver's
// TIMESTAMP-to-time.Time conversion.
func rawColumn(t *testing.T, s *Store, ip, column string) sql.NullString {
	t.Helper()
	var raw sql.NullString
	query := `SELECT CAST(` + column + ` AS TEXT) FROM banned_ips WHERE ip = ?`
	if err := s.db.QueryRow(query, ip).Scan(&raw); err != nil {
		t.Fatalf("read %s.%s: %v", ip, column, err)
	}
	return raw
}

// sqliteDatetime returns datetime(<column>) for an IP; invalid means SQLite
// could not parse the stored value.
func sqliteDatetime(t *testing.T, s *Store, ip, column string) sql.NullString {
	t.Helper()
	var parsed sql.NullString
	query := `SELECT datetime(` + column + `) FROM banned_ips WHERE ip = ?`
	if err := s.db.QueryRow(query, ip).Scan(&parsed); err != nil {
		t.Fatalf("datetime(%s) for %s: %v", column, ip, err)
	}
	return parsed
}

// State 1 (new writes): BanIP must persist a UTC "2006-01-02 15:04:05" string
// that plain SQL datetime() can read.
func TestBanIP_PersistsSQLiteReadableFormat(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	until, err := st.BanIP("1.1.1.1", time.Hour, now)
	if err != nil {
		t.Fatalf("BanIP() error = %v", err)
	}

	want := until.UTC().Format(bannedUntilTimeLayout)
	if got := rawColumn(t, st, "1.1.1.1", "banned_until"); got.String != want {
		t.Errorf("banned_until raw = %q, want %q", got.String, want)
	}
	if got := sqliteDatetime(t, st, "1.1.1.1", "banned_until"); !got.Valid || got.String != want {
		t.Errorf("datetime(banned_until) = %q (valid=%v), want %q", got.String, got.Valid, want)
	}
	if got := sqliteDatetime(t, st, "1.1.1.1", "updated_at"); !got.Valid {
		t.Errorf("datetime(updated_at) = NULL, want a parsable timestamp")
	}

	banned, err := st.IsBanned("1.1.1.1", now)
	if err != nil {
		t.Fatalf("IsBanned() error = %v", err)
	}
	if !banned {
		t.Error("IsBanned() = false, want true right after BanIP")
	}
	if banned, _ := st.IsBanned("1.1.1.1", until.Add(time.Second)); banned {
		t.Error("IsBanned() after expiry = true, want false")
	}
}

// State 1 (new writes) via the threshold path.
func TestRecordLoginFailure_PersistsSQLiteReadableFormat(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	for i := 1; i <= 3; i++ {
		banned, err := st.RecordLoginFailure("2.2.2.2", 3, time.Hour, now)
		if err != nil {
			t.Fatalf("RecordLoginFailure(#%d) error = %v", i, err)
		}
		if want := i == 3; banned != want {
			t.Fatalf("RecordLoginFailure(#%d) banned = %v, want %v", i, banned, want)
		}
	}

	want := now.Add(time.Hour).UTC().Format(bannedUntilTimeLayout)
	if got := sqliteDatetime(t, st, "2.2.2.2", "banned_until"); !got.Valid || got.String != want {
		t.Errorf("datetime(banned_until) = %q (valid=%v), want %q", got.String, got.Valid, want)
	}
	if got := sqliteDatetime(t, st, "2.2.2.2", "updated_at"); !got.Valid {
		t.Errorf("datetime(updated_at) = NULL, want a parsable timestamp")
	}
}

// State 2 (legacy reads): rows still in the old Go String format must keep
// producing correct ban decisions - neither letting a banned IP through nor
// holding an expired ban.
func TestIsBanned_ReadsLegacyGoStringFormat(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	future := now.Add(2 * time.Hour)
	past := now.Add(-2 * time.Hour)
	insertLegacyBannedIP(t, st, "3.3.3.3", future, now)
	insertLegacyBannedIP(t, st, "4.4.4.4", past, now)

	banned, err := st.IsBanned("3.3.3.3", now)
	if err != nil {
		t.Fatalf("IsBanned(legacy future) error = %v", err)
	}
	if !banned {
		t.Error("IsBanned(legacy future) = false, want true")
	}

	banned, err = st.IsBanned("4.4.4.4", now)
	if err != nil {
		t.Fatalf("IsBanned(legacy past) error = %v", err)
	}
	if banned {
		t.Error("IsBanned(legacy past) = true, want false")
	}

	list, err := st.ListBannedIPs()
	if err != nil {
		t.Fatalf("ListBannedIPs() error = %v", err)
	}
	seen := map[string]time.Time{}
	for _, b := range list {
		seen[b.IP] = b.BannedUntil
	}
	if got, ok := seen["3.3.3.3"]; !ok || got.Sub(future).Abs() > time.Second {
		t.Errorf("ListBannedIPs banned_until for legacy row = %v (found=%v), want ~%v", got, ok, future)
	}
	if got, ok := seen["4.4.4.4"]; !ok || got.Sub(past).Abs() > time.Second {
		t.Errorf("ListBannedIPs banned_until for legacy row = %v (found=%v), want ~%v", got, ok, past)
	}
}

// State 3 (rewrite): startup migration converts legacy rows in place, keeps the
// instant, leaves NULL rows alone, and is idempotent.
func TestMigrateBannedIPTimeFormat_RewritesLegacyRows(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	until := now.Add(90 * time.Minute)

	insertLegacyBannedIP(t, st, "5.5.5.5", until, now)
	if _, err := st.db.Exec(
		`INSERT INTO banned_ips (ip, fail_count, banned_until, updated_at) VALUES ('6.6.6.6', 2, NULL, ?)`,
		now.UTC().Format(bannedUntilTimeLayout),
	); err != nil {
		t.Fatalf("insert fail-count-only row: %v", err)
	}

	st.migrateBannedIPTimeFormat()

	wantUntil := until.UTC().Format(bannedUntilTimeLayout)
	if got := rawColumn(t, st, "5.5.5.5", "banned_until"); got.String != wantUntil {
		t.Errorf("rewritten banned_until = %q, want %q", got.String, wantUntil)
	}
	if got := sqliteDatetime(t, st, "5.5.5.5", "banned_until"); !got.Valid || got.String != wantUntil {
		t.Errorf("datetime(banned_until) after rewrite = %q (valid=%v), want %q", got.String, got.Valid, wantUntil)
	}
	if got := sqliteDatetime(t, st, "5.5.5.5", "updated_at"); !got.Valid {
		t.Errorf("datetime(updated_at) after rewrite = NULL, want a parsable timestamp")
	}
	if got := rawColumn(t, st, "6.6.6.6", "banned_until"); got.Valid {
		t.Errorf("NULL banned_until became %q, want it left NULL", got.String)
	}

	// Ban decisions survive the rewrite.
	banned, err := st.IsBanned("5.5.5.5", now)
	if err != nil {
		t.Fatalf("IsBanned() after rewrite error = %v", err)
	}
	if !banned {
		t.Error("IsBanned() after rewrite = false, want true")
	}

	// Second run must be a no-op, not a re-format loop.
	st.migrateBannedIPTimeFormat()
	if got := rawColumn(t, st, "5.5.5.5", "banned_until"); got.String != wantUntil {
		t.Errorf("banned_until after second run = %q, want %q", got.String, wantUntil)
	}
}

// A fail-count-only row (NULL banned_until) with a legacy updated_at still gets
// its updated_at canonicalised, and stays unbanned throughout.
func TestMigrateBannedIPTimeFormat_RewritesUpdatedAtOnNullBan(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	if _, err := st.db.Exec(
		`INSERT INTO banned_ips (ip, fail_count, banned_until, updated_at) VALUES ('9.9.9.9', 1, NULL, ?)`,
		now,
	); err != nil {
		t.Fatalf("insert legacy updated_at row: %v", err)
	}

	st.migrateBannedIPTimeFormat()

	want := now.UTC().Format(bannedUntilTimeLayout)
	if got := rawColumn(t, st, "9.9.9.9", "updated_at"); got.String != want {
		t.Errorf("updated_at after rewrite = %q, want %q", got.String, want)
	}
	if got := rawColumn(t, st, "9.9.9.9", "banned_until"); got.Valid {
		t.Errorf("banned_until = %q, want it left NULL", got.String)
	}
	if banned, err := st.IsBanned("9.9.9.9", now); err != nil || banned {
		t.Errorf("IsBanned() = %v, %v; want false, nil", banned, err)
	}
}

// Best effort: an unparsable value is skipped with a warning, other rows still
// get rewritten, and startup is never blocked (the migration returns nothing to
// fail on).
func TestMigrateBannedIPTimeFormat_SkipsUnparsableValues(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	until := now.Add(time.Hour)

	if _, err := st.db.Exec(
		`INSERT INTO banned_ips (ip, fail_count, banned_until, updated_at) VALUES ('7.7.7.7', 0, 'not-a-timestamp', 'not-a-timestamp')`,
	); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}
	insertLegacyBannedIP(t, st, "8.8.8.8", until, now)

	st.migrateBannedIPTimeFormat()

	if got := rawColumn(t, st, "7.7.7.7", "banned_until"); got.String != "not-a-timestamp" {
		t.Errorf("corrupt banned_until = %q, want it untouched", got.String)
	}
	want := until.UTC().Format(bannedUntilTimeLayout)
	if got := rawColumn(t, st, "8.8.8.8", "banned_until"); got.String != want {
		t.Errorf("legacy row after rewrite = %q, want %q", got.String, want)
	}
}
