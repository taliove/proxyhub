package server

import (
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// requireAdmin gates the /api/admin/* user management surface on the caller
// being a super admin. requireAuth must already have run (the middleware
// chain on admin routes is requireAuth(requireAdmin(handler))).
//
// Missing scope means requireAuth was bypassed — treat as 401 instead of
// silently evaluating the wrong identity. Ordinary users get 403 with a
// stable error shape so the frontend can render a friendly notice.
// mfaExemptPaths is the surface an unenrolled session may still reach, so the
// forced-enrollment gate cannot lock a user out of clearing it: the enrollment
// endpoint itself, reading own state, changing own password (a first-login user
// may owe both), and logging out.
//
// Matching is on the exact path, never a prefix: a prefix rule would let any
// route sharing an exempt path's leading segments slip through the gate.
var mfaExemptPaths = map[string]bool{
	"/api/me":            true,
	"/api/me/password":   true,
	"/api/me/mfa/enroll": true,
	"/api/logout":        true,
}

// requireMFAEnrolled blocks sessions belonging to accounts that have not bound
// an authenticator yet, returning 403 + {must_enroll_mfa: true} so the frontend
// interceptor can route to the enrollment page. MFA is mandatory for everyone,
// so "not enrolled" is a transient state every new account passes through.
//
// Enrollment state is read from the users table on every request rather than
// taken from the session payload: a super admin may reset a user's MFA while
// that user's session is live, and trusting the session would leave the gate
// open until the next login. Mirrors requirePasswordChanged, including its
// fail-open behaviour on a read error - one DB hiccup must not lock every user
// out of the whole surface.
//
// Must be chained after requireAuth (it needs the UserScope in the context).
func (s *Server) requireMFAEnrolled(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 开发环境显式放开(mfa_optional):跳过强制绑定门。生产配置永远
		// 不应开启(config.ServerConfig.MFAOptional 的注释即警告)。
		if s.cfg.Server.MFAOptional {
			next(w, r)
			return
		}
		if mfaExemptPaths[r.URL.Path] {
			next(w, r)
			return
		}
		scope, ok := UserScopeFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// The gate applies to the logged-in account, not an impersonated one:
		// a super admin's own enrollment obligation cannot be sidestepped by
		// switching view, and the viewed user's state is not the admin's to
		// clear.
		cfg, err := s.st.GetUserMFAConfig(scope.UserID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			s.logger.Warn("load mfa config for enrollment check failed, allowing",
				"user_id", scope.UserID, "error", err)
			next(w, r)
			return
		}
		if !cfg.Enabled {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{
				"must_enroll_mfa": true,
				"message":         "mfa enrollment required",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := UserScopeFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if scope.Role != store.RoleSuperAdmin {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{
				"error": "super admin required",
			})
			return
		}
		next(w, r)
	}
}
