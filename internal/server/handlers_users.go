package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// Admin user-management surface (ticket 03). All routes live under
// /api/admin/users/* and are wrapped with requireAuth + requireAdmin in
// Handler(); every handler here can assume the caller is a super admin.

// adminUserView is the JSON shape for one user row in admin endpoints.
// PassHash is never serialized (store.User marks it `json:"-"`).
type adminUserView struct {
	ID                 int64            `json:"id"`
	Username           string           `json:"username"`
	Role               string           `json:"role"`
	MustChangePassword bool             `json:"must_change_password"`
	Disabled           bool             `json:"disabled"`
	DisabledAt         *time.Time       `json:"disabled_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	LastLoginAt        *time.Time       `json:"last_login_at,omitempty"`
	Quota              *store.UserQuota `json:"quota,omitempty"`
	AirportCount       int              `json:"airport_count"`
	EndpointCount      int              `json:"endpoint_count"`
}

// toAdminUserView maps the aggregated store row onto the wire shape.
func toAdminUserView(u *store.UserWithQuotaUsage) adminUserView {
	return adminUserView{
		ID:                 u.ID,
		Username:           u.Username,
		Role:               u.Role,
		MustChangePassword: u.MustChangePassword,
		Disabled:           u.Disabled(),
		DisabledAt:         u.DisabledAt,
		CreatedAt:          u.CreatedAt,
		LastLoginAt:        u.LastLoginAt,
		Quota:              u.Quota,
		AirportCount:       u.AirportCount,
		EndpointCount:      u.EndpointCount,
	}
}

// generatePassword returns a random 16-character alphanumeric password
// (mixed case + digits). Used by reset-password so admins receive a
// credential they can hand to the user out of band. Draws via rand.Int to
// avoid modulo bias (256 % 62 != 0).
func generatePassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 16)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	return string(buf), nil
}

// parseAdminUserID pulls the {id} path value as a positive int64.
func parseAdminUserID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid user id")
	}
	return id, nil
}

// handleAdminListUsers serves GET /api/admin/users: the full user list with
// per-user quota and current airport/endpoint usage counts.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsersWithQuotaUsage()
	if err != nil {
		s.logger.Error("list users failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	views := make([]adminUserView, 0, len(users))
	for _, u := range users {
		views = append(views, toAdminUserView(u))
	}
	writeJSON(w, views)
}

// adminCreateUserRequest is the POST /api/admin/users payload.
type adminCreateUserRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Role          string `json:"role"`            // optional; defaults to store.RoleUser
	MaxAirports   int    `json:"max_airports"`    // optional quota; 0 means "no cap configured"
	MaxEndpoints  int    `json:"max_endpoints"`   // optional quota
	XrayPortStart int    `json:"xray_port_start"` // optional; 0/0 means "no range yet"
	XrayPortEnd   int    `json:"xray_port_end"`
	StartXray     bool   `json:"start_xray"` // optionally boot the user's Xray right away
}

// handleAdminCreateUser serves POST /api/admin/users: create a user with
// an initial password and optional quota. Reserved usernames are rejected
// at the store layer; here we translate domain errors into 4xx responses.
func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req adminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 密码复杂度规则与自助改密一致(8 位 + 字母数字,见 handlers_me.go)。
	if err := validateNewPassword(req.Password); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	role := req.Role
	if role == "" {
		role = store.RoleUser
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.logger.Error("hash password failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := s.st.CreateUser(req.Username, string(hash), role, true)
	if err != nil {
		s.writeUserStoreError(w, err)
		return
	}

	// Always land a quota row so downstream (xraymgr) can rely on it existing.
	quota := &store.UserQuota{
		UserID:        user.ID,
		MaxAirports:   req.MaxAirports,
		MaxEndpoints:  req.MaxEndpoints,
		XrayPortStart: req.XrayPortStart,
		XrayPortEnd:   req.XrayPortEnd,
	}
	if err := s.st.UpsertUserQuota(quota); err != nil {
		s.logger.Error("upsert user quota failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Optionally boot the user's Xray instance. Failure does not roll back
	// the user creation: the admin can restart it later from the xray panel.
	if req.StartXray && s.xrayManager() != nil {
		if _, err := s.xrayManager().Start(r.Context(), user.ID); err != nil {
			s.logger.Warn("auto-start user xray failed", "user_id", user.ID, "error", err)
		}
	}

	s.recordAudit("admin_create_user", s.clientIP(r), req.Username,
		fmt.Sprintf("创建用户 id=%d role=%s", user.ID, user.Role),
		r.UserAgent())

	writeJSONStatus(w, http.StatusCreated, toAdminUserView(&store.UserWithQuotaUsage{
		User:  *user,
		Quota: quota,
	}))
}

// handleAdminGetUser serves GET /api/admin/users/{id}.
func (s *Server) handleAdminGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	user, err := s.st.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	quota, qerr := s.st.GetUserQuota(id)
	if qerr != nil && !errors.Is(qerr, store.ErrNotFound) {
		s.logger.Error("get user quota failed", "user_id", id, "error", qerr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, toAdminUserView(&store.UserWithQuotaUsage{User: *user, Quota: quota}))
}

// adminUpdateUserRequest is the PUT /api/admin/users/{id} payload. Pointer
// fields let the caller patch only what they send; nil means "leave alone".
type adminUpdateUserRequest struct {
	Role          *string `json:"role"`
	MaxAirports   *int    `json:"max_airports"`
	MaxEndpoints  *int    `json:"max_endpoints"`
	XrayPortStart *int    `json:"xray_port_start"`
	XrayPortEnd   *int    `json:"xray_port_end"`
}

// handleAdminUpdateUser serves PUT /api/admin/users/{id}: patch role and/or
// quota. Pass-hash changes go through reset-password (separate endpoint).
func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var req adminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Role == nil && req.MaxAirports == nil && req.MaxEndpoints == nil &&
		req.XrayPortStart == nil && req.XrayPortEnd == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	if _, err := s.st.GetUserByID(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if req.Role != nil {
		if err := s.st.UpdateUser(id, store.UserUpdate{Role: req.Role}); err != nil {
			s.writeUserStoreError(w, err)
			return
		}
	}

	// Merge quota updates onto the existing row so partial patches don't
	// zero out untouched fields.
	quota, qerr := s.st.GetUserQuota(id)
	if qerr != nil {
		if errors.Is(qerr, store.ErrNotFound) {
			quota = &store.UserQuota{UserID: id}
		} else {
			s.logger.Error("get user quota failed", "user_id", id, "error", qerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if req.MaxAirports != nil {
		quota.MaxAirports = *req.MaxAirports
	}
	if req.MaxEndpoints != nil {
		quota.MaxEndpoints = *req.MaxEndpoints
	}
	if req.XrayPortStart != nil {
		quota.XrayPortStart = *req.XrayPortStart
	}
	if req.XrayPortEnd != nil {
		quota.XrayPortEnd = *req.XrayPortEnd
	}
	if req.MaxAirports != nil || req.MaxEndpoints != nil ||
		req.XrayPortStart != nil || req.XrayPortEnd != nil {
		if err := s.st.UpsertUserQuota(quota); err != nil {
			s.writeUserStoreError(w, err)
			return
		}
	}

	s.recordAudit("admin_update_user", s.clientIP(r), strconv.FormatInt(id, 10),
		fmt.Sprintf("更新用户 id=%d", id),
		r.UserAgent())

	user, err := s.st.GetUserByID(id)
	if err != nil {
		// 更新已生效,读回失败只影响响应体:记日志并返回不带 profile 的 200。
		s.logger.Error("read back updated user failed", "user_id", id, "error", err)
		writeJSON(w, map[string]any{"ok": true, "id": id})
		return
	}
	writeJSON(w, toAdminUserView(&store.UserWithQuotaUsage{User: *user, Quota: quota}))
}

// handleAdminDisableUser serves POST /api/admin/users/{id}/disable.
// Stops the user's Xray (account no longer relays traffic) then marks the
// row disabled. Xray stop is best-effort: a missing instance is fine.
func (s *Server) handleAdminDisableUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if _, err := s.st.GetUserByID(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if mgr := s.xrayManager(); mgr != nil {
		if err := mgr.HandleUserDisabled(r.Context(), id); err != nil {
			s.logger.Warn("stop user xray on disable failed", "user_id", id, "error", err)
		}
	}

	if err := s.st.DisableUser(id, time.Now()); err != nil {
		s.logger.Error("disable user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// 吊销该用户全部存活会话:禁用即切断,不留 12h TTL 残余访问。
	s.sessions.DestroyForUser(id)
	s.recordAudit("admin_disable_user", s.clientIP(r), strconv.FormatInt(id, 10),
		fmt.Sprintf("禁用用户 id=%d", id),
		r.UserAgent())
	writeJSON(w, map[string]bool{"ok": true, "disabled": true})
}

// handleAdminEnableUser serves POST /api/admin/users/{id}/enable.
func (s *Server) handleAdminEnableUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if err := s.st.EnableUser(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("enable user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.recordAudit("admin_enable_user", s.clientIP(r), strconv.FormatInt(id, 10),
		fmt.Sprintf("启用用户 id=%d", id),
		r.UserAgent())
	writeJSON(w, map[string]bool{"ok": true, "disabled": false})
}

// handleAdminDeleteUser serves DELETE /api/admin/users/{id}: physical delete
// with cascade across every per-user table. audit_logs are preserved
// (audit trail must survive the account) but re-attributed to the caller.
//
// Deleting the currently-authenticated super admin is refused: that would
// strand the deployment with no way back in. Deleting any other super admin
// is allowed (the caller remains).
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	if scope.UserID == id {
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "cannot delete your own account",
		})
		return
	}

	user, err := s.st.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Tear down the Xray process first: it holds the user's port and its
	// working directory, both of which must be gone before the row drops.
	if mgr := s.xrayManager(); mgr != nil {
		if err := mgr.HandleUserDeleted(r.Context(), id); err != nil {
			s.logger.Warn("delete user xray failed", "user_id", id, "error", err)
		}
	}

	if err := s.st.DeleteUserCascade(id); err != nil {
		s.logger.Error("cascade delete user data failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.sessions.DestroyForUser(id)

	s.recordAudit("admin_delete_user", s.clientIP(r), user.Username,
		fmt.Sprintf("删除用户 id=%d role=%s", id, user.Role),
		r.UserAgent())
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAdminResetPassword serves POST /api/admin/users/{id}/reset-password.
// Generates a fresh 16-char password, stores its bcrypt hash, marks
// must_change_password=true, and returns the plaintext to the caller.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	user, err := s.st.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pw, err := generatePassword()
	if err != nil {
		s.logger.Error("generate password failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		s.logger.Error("hash password failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	mustChange := true
	hashStr := string(hash)
	if err := s.st.UpdateUser(id, store.UserUpdate{
		PassHash:           &hashStr,
		MustChangePassword: &mustChange,
	}); err != nil {
		s.logger.Error("update user password failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// 凭证轮换后吊销该用户全部会话:被窃会话不得继续有效。
	s.sessions.DestroyForUser(id)

	s.recordAudit("admin_reset_password", s.clientIP(r), user.Username,
		fmt.Sprintf("重置用户 id=%d 的密码", id),
		r.UserAgent())
	writeJSON(w, map[string]any{
		"ok":       true,
		"password": pw,
		"user_id":  id,
		"username": user.Username,
	})
}

// writeUserStoreError translates store-layer sentinel errors from the users
// surface into HTTP responses. Falls through to 500 for unexpected errors.
func (s *Server) writeUserStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, store.ErrUsernameTaken):
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "username already taken",
		})
	case errors.Is(err, store.ErrUsernameReserved):
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "username is reserved",
		})
	case errors.Is(err, store.ErrInvalidRole):
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "invalid role",
		})
	case errors.Is(err, store.ErrInvalidInput):
		msg := err.Error()
		// Strip the sentinel prefix so the client sees the actionable part.
		msg = strings.TrimPrefix(msg, store.ErrInvalidInput.Error()+": ")
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": msg})
	default:
		s.logger.Error("user store error", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
