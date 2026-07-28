package store

import (
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

// legacyBannedUntilLayouts are the shapes banned_until can carry on disk from
// before the UTC-string fix. The first entry is what the modernc driver
// produces from time.Time.String() once the monotonic-clock suffix ("m=+...")
// is trimmed; the rest cover RFC3339 rows written by other paths.
var legacyBannedUntilLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999-07:00",
	time.RFC3339Nano,
	time.RFC3339,
}

// parseBannedUntil parses a banned_until value in either the current UTC
// "2006-01-02 15:04:05" format or any legacy format previously written by the
// driver. Values without a zone (the current format) are read as UTC, matching
// how they are written. Returns ok=false when nothing matches, so callers can
// warn instead of silently treating garbage as a zero time (which would read as
// "not banned" for bans and "banned forever" for nothing).
func parseBannedUntil(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.ParseInLocation(bannedUntilTimeLayout, raw, time.UTC); err == nil {
		return t, true
	}
	// time.Time.String() appends " m=+0.000000001" for values carrying a
	// monotonic reading; that suffix is not part of any parsable layout.
	trimmed := raw
	if idx := strings.Index(raw, " m="); idx > 0 {
		trimmed = raw[:idx]
	}
	for _, layout := range legacyBannedUntilLayouts {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// migrateBannedIPTimeFormat rewrites banned_ips rows whose timestamps are still
// in a legacy format into the UTC "2006-01-02 15:04:05" shape that SQLite's
// datetime() understands (ADR 0010). Reads happen through parseBannedUntil, so
// this is a cosmetic-plus-queryability cleanup, not a correctness prerequisite.
//
// Best effort by design: every failure is logged and skipped, and the function
// returns nothing, so a broken row can never block startup. Idempotent - rows
// already in the target format are left untouched.
func (s *Store) migrateBannedIPTimeFormat() {
	type rewrite struct {
		ip          string
		bannedUntil sql.NullString
		updatedAt   sql.NullString
	}

	rows, err := s.db.Query(
		`SELECT ip, CAST(banned_until AS TEXT), CAST(updated_at AS TEXT) FROM banned_ips`,
	)
	if err != nil {
		slog.Warn("banned_ips time format migration: query failed, skipping", "error", err)
		return
	}

	var pending []rewrite
	for rows.Next() {
		var r rewrite
		if err := rows.Scan(&r.ip, &r.bannedUntil, &r.updatedAt); err != nil {
			slog.Warn("banned_ips time format migration: scan failed, skipping row", "error", err)
			continue
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("banned_ips time format migration: row iteration failed", "error", err)
	}
	// Close before the UPDATE loop: the store caps SQLite at a single
	// connection, so an open read cursor would deadlock the writes.
	rows.Close()

	rewritten := 0
	for _, r := range pending {
		bannedUntil, bannedChanged := normalizedBannedTime(r.ip, "banned_until", r.bannedUntil)
		updatedAt, updatedChanged := normalizedBannedTime(r.ip, "updated_at", r.updatedAt)
		if !bannedChanged && !updatedChanged {
			continue
		}
		if _, err := s.db.Exec(
			`UPDATE banned_ips SET banned_until = ?, updated_at = ? WHERE ip = ?`,
			bannedUntil, updatedAt, r.ip,
		); err != nil {
			slog.Warn("banned_ips time format migration: rewrite failed, leaving row as-is",
				"ip", r.ip, "error", err)
			continue
		}
		rewritten++
	}
	if rewritten > 0 {
		slog.Info("banned_ips time format migration: rewrote legacy rows", "count", rewritten)
	}
}

// normalizedBannedTime converts one stored timestamp into the canonical UTC
// string. It returns the value to persist plus whether it differs from what is
// already on disk. NULL stays NULL; unparsable text is preserved verbatim and
// reported, so operators can inspect it instead of losing it to a rewrite.
func normalizedBannedTime(ip, column string, stored sql.NullString) (any, bool) {
	if !stored.Valid {
		return nil, false
	}
	if stored.String == "" {
		return stored.String, false
	}
	if _, err := time.ParseInLocation(bannedUntilTimeLayout, stored.String, time.UTC); err == nil {
		return stored.String, false // already canonical
	}
	parsed, ok := parseBannedUntil(stored.String)
	if !ok {
		slog.Warn("banned_ips time format migration: unparsable value, skipping",
			"ip", ip, "column", column, "value", stored.String)
		return stored.String, false
	}
	return parsed.UTC().Format(bannedUntilTimeLayout), true
}
