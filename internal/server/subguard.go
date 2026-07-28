package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// Subscription pull guard chain (pull-guard ticket 04).
//
// IRON RULE - ordering relative to authentication:
// guards run *only after* the /sub path lookup, the constant-time token
// comparison and the enabled check in handleSubscription have all passed. A
// request with an unknown path, a wrong token or a disabled address never
// reaches a guard: it keeps getting the uniform stock 404, so a guard response
// (429, 403, ...) is by construction only observable by a client that already
// holds a valid path+token pair. Guards must therefore never be called from
// anywhere but runSubGuards, and runSubGuards must never be called before that
// validation section. TestSubGuards_InvalidTokenSkipsGuards pins this down.
//
// A guard decides one of three things about the request:
//   - allow it, silently (allowPull)
//   - allow it, but leave a pull_logs trace (observePull - used by the geo
//     guard's observe mode in ticket 07)
//   - block it, writing its own response and status (blockPull)
//
// The chain stops at the first blocking verdict; the remaining guards do not
// run, so at most one blocked status is recorded per request.
type subGuard interface {
	// name identifies the guard in logs. Stable string, not user facing.
	name() string
	// check inspects one already-authenticated pull request.
	check(gr subGuardRequest) subGuardVerdict
}

// subGuardRequest is everything a guard is allowed to look at. It carries the
// resolved endpoint and the already-normalised client IP so guards cannot
// disagree about which address a request came from (clientIP only trusts
// X-Forwarded-For behind a loopback peer).
type subGuardRequest struct {
	req *http.Request
	ep  *store.Endpoint
	ip  string
}

// subGuardVerdict is a guard's answer for one request.
type subGuardVerdict struct {
	// status, when non-empty, is recorded in pull_logs for this request. It
	// must be one of the store.PullStatus* constants. A blocking verdict is
	// expected to always carry one; an allowing verdict carries one only when
	// the guard wants an observability trace without changing the outcome.
	status string
	// block stops the chain and suppresses the subscription payload.
	block bool
	// respond writes the client-visible answer for a blocking verdict. Nil
	// means the uniform stock 404, which is the safe default for any guard
	// that does not want to reveal that it exists.
	respond func(w http.ResponseWriter)
}

// allowPull lets the request through without a trace of its own. The success
// path still records PullStatusOK once the payload is generated.
func allowPull() subGuardVerdict { return subGuardVerdict{} }

// observePull lets the request through but records status. Used by dry-run
// modes (ticket 07 geo observe) that must be measurable before being enforced.
func observePull(status string) subGuardVerdict {
	return subGuardVerdict{status: status}
}

// blockPull stops the chain, records status and writes respond. Pass a nil
// respond to fall back to the uniform 404.
func blockPull(status string, respond func(w http.ResponseWriter)) subGuardVerdict {
	return subGuardVerdict{status: status, block: true, respond: respond}
}

// newSubGuardChain builds the ordered guard chain for this server.
//
// Order is registration order, and it is part of the behaviour: the first
// blocking guard wins, so the recorded status of a request that trips several
// guards is the one of the earliest guard in this slice. Cheap in-memory checks
// come before checks that hit the database or the GeoIP table.
//
// Adding a guard (tickets 05 / 07) is a new file plus one line here:
//
//	return []subGuard{
//	    newRateLimitGuard(s.pullRateLimit, s.pullRateThreshold.get), // ticket 04
//	    newPullBlacklistGuard(s.st, s.logger),  // ticket 05: scope=sub IP rules -> 403 blacklisted
//	    newGeoAllowlistGuard(s.geo),            // ticket 07: off / observe / enforce -> geo_* statuses
//	}
//
// A guard that needs a DB-backed setting must read it through a cache like
// pullRateThreshold: the chain runs on every pull including the rejected ones,
// so a per-request query would let an abusive client amplify its own load.
//
// Each new guard only has to implement subGuard and return the right verdict;
// recording the status and writing the response stays here, so a guard cannot
// forget to leave a trace or accidentally answer twice.
func (s *Server) newSubGuardChain() []subGuard {
	return []subGuard{
		newRateLimitGuard(s.pullRateLimit, s.pullRateThreshold.get),
	}
}

// runSubGuards runs the chain for an authenticated pull request and reports
// whether handleSubscription may continue to generation.
//
// It owns both side effects a verdict implies: the pull_logs trace and the
// client response. A blocked request gets exactly one response written here and
// the caller returns immediately.
func (s *Server) runSubGuards(w http.ResponseWriter, r *http.Request, ep *store.Endpoint) bool {
	gr := subGuardRequest{req: r, ep: ep, ip: clientIP(r)}

	for _, g := range s.subGuards {
		verdict := g.check(gr)
		if verdict.status != "" {
			s.recordPullStatus(r, ep.ID, verdict.status)
		}
		if !verdict.block {
			continue
		}
		s.logger.Info("subscription pull blocked",
			"guard", g.name(), "status", verdict.status,
			"endpoint_id", ep.ID, "ip", gr.ip)
		if verdict.respond != nil {
			verdict.respond(w)
		} else {
			http.NotFound(w, r)
		}
		return false
	}
	return true
}
