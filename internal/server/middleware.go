package server

import (
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
