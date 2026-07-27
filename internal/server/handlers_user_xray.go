package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/xraymgr"
)

// xrayStatusView is the JSON shape returned by the per-user Xray status
// endpoints. It mirrors xraymgr.Status but lives in the server package so
// the HTTP surface can evolve independently of the manager's internal
// struct.
type xrayStatusView struct {
	UserID        int64  `json:"user_id"`
	Port          int    `json:"port"`
	ConfigPath    string `json:"config_path"`
	PID           int    `json:"pid"`
	Status        string `json:"status"`
	LastStartedAt string `json:"last_started_at,omitempty"`
	ProcessAlive  bool   `json:"process_alive"`
}

// toXrayStatusView maps the manager status to the wire format.
func toXrayStatusView(s *xraymgr.Status) xrayStatusView {
	v := xrayStatusView{
		UserID:       s.UserID,
		Port:         s.Port,
		ConfigPath:   s.ConfigPath,
		PID:          s.PID,
		Status:       s.Status,
		ProcessAlive: s.ProcessAlive,
	}
	if s.LastStartedAt != nil {
		v.LastStartedAt = s.LastStartedAt.Format("2006-01-02 15:04:05")
	}
	return v
}

// xrayManager returns the per-user Xray manager wired at server startup.
// Returns nil when the manager is not configured (tests that don't need it
// construct the Server without one); handlers must nil-check before use.
func (s *Server) xrayManager() *xraymgr.Manager {
	return s.xrayMgr
}

// handleGetMyXray serves GET /api/me/xray: the authenticated user reads
// their own Xray status.
//
// Auth context (ticket 02) is not yet wired into sessions; until then this
// endpoint resolves the caller through the legacy single-admin identity
// (the only user that can exist today). When ticket 02 lands, the user id
// comes from the session instead.
func (s *Server) handleGetMyXray(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.writeXrayStatusForUser(w, r, userID)
}

// handleGetUserXray serves GET /api/admin/users/{id}/xray: super admins
// read any user's Xray status.
func (s *Server) handleGetUserXray(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	s.writeXrayStatusForUser(w, r, userID)
}

// handleRestartUserXray serves POST /api/admin/users/{id}/xray/restart:
// super admins bounce a user's Xray process. The restart is synchronous so
// the caller sees the final state.
func (s *Server) handleRestartUserXray(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	mgr := s.xrayManager()
	if mgr == nil {
		http.Error(w, "xray manager not configured", http.StatusServiceUnavailable)
		return
	}

	st, err := mgr.Restart(r.Context(), userID)
	if err != nil {
		s.writeXrayError(w, err)
		return
	}
	writeJSON(w, toXrayStatusView(st))
}

// writeXrayStatusForUser fetches and serializes one user's status.
func (s *Server) writeXrayStatusForUser(w http.ResponseWriter, r *http.Request, userID int64) {
	mgr := s.xrayManager()
	if mgr == nil {
		http.Error(w, "xray manager not configured", http.StatusServiceUnavailable)
		return
	}
	st, err := mgr.GetStatus(r.Context(), userID)
	if err != nil {
		s.writeXrayError(w, err)
		return
	}
	writeJSON(w, toXrayStatusView(st))
}

// writeXrayError maps manager/store errors onto HTTP status codes.
// Kept as one place so all three endpoints stay consistent.
func (s *Server) writeXrayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]string{
			"error": "no xray instance for user",
		})
	case errors.Is(err, xraymgr.ErrQuotaMissing):
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "user has no xray port range configured",
		})
	case errors.Is(err, xraymgr.ErrNoFreePort):
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "no free port in user xray range",
		})
	default:
		s.logger.Error("xray handler error", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// currentUserID resolves the caller's user id from the request.
//
// This is a seam for ticket 02 (auth & session). Until session-backed user
// identity lands, the only resolvable identity is the legacy single admin:
// the caller must present a valid session cookie and the user id is the
// first super_admin row. Once ticket 02 ships, this helper reads the user
// id out of the session and the rest of the call graph stays unchanged.
func (s *Server) currentUserID(r *http.Request) (int64, error) {
	// Ticket 07: requireAuth 注入的 context scope 是权威来源(新会话/旧会话已归一)。
	if scope, ok := UserScopeFromContext(r.Context()); ok {
		return scope.UserID, nil
	}
	// 直接调 handler 的测试可能绕过 requireAuth:回退为校验 cookie + 首个 super_admin,
	// 保持注入前的行为不变,避免直接调用逃逸鉴权边界。
	cookie, err := r.Cookie("session")
	if err != nil || !s.sessions.Validate(cookie.Value) {
		return 0, errors.New("no valid session")
	}
	return s.firstSuperAdminID()
}

// requireSuperAdmin gates admin-only handlers on the caller being a super
// admin. Until ticket 02 lands, any authenticated caller is the single
// super admin (the legacy single-admin deployment); afterwards this checks
// the session's role claim.
//
// Returns true when the caller is allowed, false after writing the error.
func (s *Server) requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID, err := s.currentUserID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	u, err := s.st.GetUserByID(userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if u.Role != store.RoleSuperAdmin {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{
			"error": "super admin required",
		})
		return false
	}
	return true
}

// Ensure encoding/json stays referenced (used by tests that decode the
// wire shape from this file).
var _ = json.Marshal
