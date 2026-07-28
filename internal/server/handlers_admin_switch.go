package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// Admin switch-user surface (ticket 09): super admins can enter another
// user's space (impersonation) and exit back to their own view. The acting
// target is persisted on the session so every subsequent request resolves
// through requireAuth without a query parameter.

// adminSwitchUserRequest is the POST /api/admin/switch-user payload.
type adminSwitchUserRequest struct {
	UserID int64 `json:"user_id"`
}

// adminCurrentViewResponse mirrors the effective UserScope plus the acting
// user's profile so the frontend can render "viewing as X" without a second
// round trip.
type adminCurrentViewResponse struct {
	UserID         int64         `json:"user_id"`
	Username       string        `json:"username"`
	Role           string        `json:"role"`
	ActingUserID   int64         `json:"acting_user_id"`
	ActingUsername string        `json:"acting_username,omitempty"`
	Acting         bool          `json:"acting"`
	Profile        adminUserView `json:"profile"`
}

// currentSessionToken extracts the raw session cookie value; required to
// mutate the acting target on the session. Missing cookie means requireAuth
// was bypassed (should be unreachable on guarded routes).
func currentSessionToken(r *http.Request) (string, bool) {
	c, err := r.Cookie("session")
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// resolveUserView loads a user + quota and wraps it in the admin wire shape.
// Returns store.ErrNotFound when the user no longer exists.
func (s *Server) resolveUserView(id int64) (adminUserView, error) {
	user, err := s.st.GetUserByID(id)
	if err != nil {
		return adminUserView{}, err
	}
	quota, qerr := s.st.GetUserQuota(id)
	if qerr != nil && !errors.Is(qerr, store.ErrNotFound) {
		return adminUserView{}, qerr
	}
	return toAdminUserView(&store.UserWithQuotaUsage{User: *user, Quota: quota}), nil
}

// handleAdminSwitchUser serves POST /api/admin/switch-user: super admin
// enters the target user's space. The target must exist; passing the
// admin's own id is a no-op (acting_user_id cleared).
func (s *Server) handleAdminSwitchUser(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	var req adminSwitchUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.UserID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "user_id must be positive",
		})
		return
	}

	target, err := s.st.GetUserByID(req.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{
				"error": "user not found",
			})
			return
		}
		s.logger.Error("switch-user lookup failed", "user_id", req.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if target.Disabled() {
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "cannot enter a disabled user's space",
		})
		return
	}

	token, ok := currentSessionToken(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Switching to self is equivalent to exiting: persist 0 so the scope
	// resolves to the admin's own id on subsequent requests.
	acting := req.UserID
	if req.UserID == scope.UserID {
		acting = 0
	}
	if !s.sessions.SetActingUser(token, acting) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.recordAudit("admin_switch_user", clientIP(r), target.Username,
		"超管进入用户空间", r.UserAgent())

	view, verr := s.resolveUserView(req.UserID)
	if verr != nil {
		s.logger.Error("resolve switched user view failed", "user_id", req.UserID, "error", verr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

// handleAdminExitSwitch serves POST /api/admin/exit-switch: clear the
// session's acting_user_id so subsequent requests resolve to the admin's
// own scope. Idempotent: exiting with no active switch is a no-op.
func (s *Server) handleAdminExitSwitch(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	token, ok := currentSessionToken(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.sessions.SetActingUser(token, 0) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if scope.ActingUserID > 0 {
		s.recordAudit("admin_exit_switch", clientIP(r), "", "超管退出用户空间", r.UserAgent())
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAdminCurrentView serves GET /api/admin/current-view: report the
// caller's effective scope. Super admins see the acting target (if any);
// ordinary users see themselves with acting=false.
func (s *Server) handleAdminCurrentView(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	me, err := s.resolveUserView(scope.UserID)
	if err != nil {
		s.logger.Error("resolve self view failed", "user_id", scope.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := adminCurrentViewResponse{
		UserID:   scope.UserID,
		Username: me.Username,
		Role:     scope.Role,
		Profile:  me,
	}

	if scope.IsSuperAdmin() && scope.ActingUserID > 0 && scope.ActingUserID != scope.UserID {
		actingView, aerr := s.resolveUserView(scope.ActingUserID)
		if aerr != nil {
			// Acting target was deleted mid-session: fall back to self view
			// rather than failing the request; the frontend will re-render
			// the non-impersonating navbar on next load.
			s.logger.Warn("acting target missing, clearing view",
				"acting_user_id", scope.ActingUserID, "error", aerr)
			resp.Acting = false
		} else {
			resp.Acting = true
			resp.ActingUserID = scope.ActingUserID
			resp.ActingUsername = actingView.Username
			resp.Profile = actingView
		}
	}
	writeJSON(w, resp)
}
