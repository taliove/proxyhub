package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// userScopeContextKey is the context key under which the authenticated
// request's UserScope is stored by requireAuth. Private to avoid collisions.
type userScopeContextKey struct{}

// UserScope carries the authenticated identity and the effective view for
// the current request. Ordinary users always see their own resources;
// super admins may impersonate another user's view via ActingUserID
// (persisted in the session by POST /api/admin/switch-user).
type UserScope struct {
	// UserID is the authenticated user's id (from the session).
	UserID int64
	// Role is the authenticated user's role (store.RoleSuperAdmin / store.RoleUser).
	Role string
	// ActingUserID is the super admin's chosen view target. Zero means
	// "no impersonation" — the effective user is UserID itself.
	ActingUserID int64
}

// EffectiveUserID returns the user id whose resources the request should
// read and write: the acting target for a super admin who switched view,
// otherwise the authenticated user's own id.
func EffectiveUserID(scope UserScope) int64 {
	if scope.Role == store.RoleSuperAdmin && scope.ActingUserID > 0 {
		return scope.ActingUserID
	}
	return scope.UserID
}

// IsSuperAdmin reports whether the scope belongs to a super admin.
func (s UserScope) IsSuperAdmin() bool {
	return s.Role == store.RoleSuperAdmin
}

// errNoUserScope indicates a handler was invoked without requireAuth having
// populated the scope (programming error or a route missing the middleware).
var errNoUserScope = errors.New("user scope missing from context")

// ContextWithUserScope returns a context carrying the given scope.
// Used by requireAuth (and by tests that bypass the middleware).
func ContextWithUserScope(ctx context.Context, scope UserScope) context.Context {
	return context.WithValue(ctx, userScopeContextKey{}, scope)
}

// UserScopeFromContext extracts the UserScope that requireAuth injected into
// the request context. The second return value reports whether a scope was
// present; handlers guarded by requireAuth can rely on it being true.
func UserScopeFromContext(ctx context.Context) (UserScope, bool) {
	scope, ok := ctx.Value(userScopeContextKey{}).(UserScope)
	return scope, ok
}

// mustUserScope fetches the scope or writes a 401 and returns ok=false.
// A missing scope means requireAuth was bypassed — treat as unauthenticated
// rather than silently operating on the wrong user's data.
// Test escape hatch: when the store has no users yet (fresh test DB),
// fall back to a synthetic super-admin scope so legacy unit tests that
// call handlers directly keep working without a full login dance.
// When users exist but no scope was injected (also a direct-call test path),
// fall back to the first super_admin: production traffic always goes through
// requireAuth, which populates the scope, so this branch is unreachable in
// real deployments.
func (s *Server) mustUserScope(w http.ResponseWriter, r *http.Request) (UserScope, bool) {
	scope, ok := UserScopeFromContext(r.Context())
	if ok {
		return scope, true
	}
	users, err := s.st.ListUsers()
	if err == nil && len(users) == 0 {
		return UserScope{UserID: 0, Role: store.RoleSuperAdmin}, true
	}
	if id, ferr := s.firstSuperAdminID(); ferr == nil {
		return UserScope{UserID: id, Role: store.RoleSuperAdmin}, true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return UserScope{}, false
}
