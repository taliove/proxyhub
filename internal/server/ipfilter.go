package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// ipFilterMiddleware enforces the site-wide deny list (pull guard ticket 03).
//
// A request whose source matches a live scope=global rule gets the same plain
// 404 that a request outside the Site Path boundary gets: no admin surface, no
// subscription endpoint, no health check, no distinguishable error. Uniformity
// is the point - a banned scanner must not be able to tell a ban from a
// non-existent host, so this runs before the router (inside sitePathMiddleware)
// and never branches per path.
//
// Loopback is intentionally not special-cased here: store.IsDenied already
// exempts it, keeping the escape-hatch rule in one place.
//
// The middleware fails open. A deny list is defense in depth (authentication,
// rate limiting and the pull guard chain all still run), so a SQLite hiccup
// costs one un-filtered request instead of 404-ing the whole site - including
// the operator's own way back in.
func (s *Server) ipFilterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		denied, err := s.st.IsDenied(ip, store.IPRuleScopeGlobal)
		if err != nil {
			s.logger.Warn("global ip deny check failed, allowing request",
				"ip", ip, "error", err)
			next.ServeHTTP(w, r)
			return
		}
		if denied {
			// Debug level on purpose: a banned source can retry endlessly and
			// must not be able to fill the operator's log.
			s.logger.Debug("request denied by global ip rule", "ip", ip)
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
