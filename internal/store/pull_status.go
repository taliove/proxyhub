package store

// Pull outcome statuses recorded on every pull_logs row (pull-guard ticket 01).
// A pull log row is no longer proof of a successful delivery: blocked attempts
// leave a trace too, so the reason lives in the row itself.
//
// Only PullStatusOK / PullStatusDisabled / PullStatusBadToken are produced as of
// ticket 01. The four guard statuses are declared here up front so the later
// guard tickets (rate limit / blacklist / geo allowlist) only add a guard, not a
// schema or recording change.
const (
	// PullStatusOK a subscription payload was actually delivered.
	PullStatusOK = "ok"
	// PullStatusRateLimited the per-IP x per-endpoint pull rate limit tripped.
	PullStatusRateLimited = "rate_limited"
	// PullStatusGeoBlocked the geo allowlist rejected the client in enforce mode.
	PullStatusGeoBlocked = "geo_blocked"
	// PullStatusGeoWouldBlock the geo allowlist would have rejected the client,
	// but the endpoint runs in observe mode so the pull was served.
	PullStatusGeoWouldBlock = "geo_would_block"
	// PullStatusBlacklisted an IP rule with scope=sub matched the client.
	PullStatusBlacklisted = "blacklisted"
	// PullStatusDisabled the subscription address exists but is disabled.
	PullStatusDisabled = "disabled"
	// PullStatusBadToken unknown /sub path, or a path that exists with a token
	// that does not match. Both answer a uniform 404 to the client.
	PullStatusBadToken = "bad_token"
	// PullStatusGraceOK the pull was served via the previous-generation link
	// while the reset grace window is still alive(issue #117):与正常 ok 区分,
	// 供管理员观察蹭用是否在迁移窗口内消失。
	PullStatusGraceOK = "grace_ok"
)

// pullStatuses is the closed set accepted by RecordPull. Keeping it closed makes
// a typo in a guard fail loudly at write time instead of silently producing a
// status value no reader (UI filter, aggregate query) knows about.
var pullStatuses = map[string]struct{}{
	PullStatusOK:            {},
	PullStatusRateLimited:   {},
	PullStatusGeoBlocked:    {},
	PullStatusGeoWouldBlock: {},
	PullStatusBlacklisted:   {},
	PullStatusDisabled:      {},
	PullStatusBadToken:      {},
	PullStatusGraceOK:       {},
}

// IsValidPullStatus reports whether status is a known pull outcome.
func IsValidPullStatus(status string) bool {
	_, ok := pullStatuses[status]
	return ok
}
