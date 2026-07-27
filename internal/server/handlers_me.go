package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// 自助改密(ticket 04):当前登录用户凭旧密码换新密码。
// 规则:新密码 >= 8 位且同时含字母与数字;改密成功清除 must_change_password,
// 并销毁当前会话 —— 前端收到 401 后自然回到登录页,避免旧会话残留已过期权限位。

// passwordMinLength 自助改密的最小长度(ticket 04 契约:8 位 + 字母数字)。
// 与初始化向导的 12 位下限不同:那里是超管首设,这里是普通用户日常改密。
const passwordMinLength = 8

// validateNewPassword 校验新密码复杂度:长度 >= passwordMinLength,
// 且至少含一个字母与一个数字。返回的 error 文本直接回给前端展示。
func validateNewPassword(pw string) error {
	if len(pw) < passwordMinLength {
		return errors.New("password must be at least 8 characters")
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("password must contain both letters and digits")
	}
	return nil
}

// handleChangeMyPassword serves POST /api/me/password.
// 请求 {old_password, new_password};bcrypt 校验旧密码(失败 400,不区分
// "用户不存在"——用户已登录,旧密码错就是凭据错),新密码复杂度不足 400。
// 成功后销毁当前会话:权限位(must_change_password)随会话签发,旧会话
// 继续持有旧位会造成"已改密但仍被拦"或"未改密却放行"的不一致。
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		http.Error(w, "old_password and new_password are required", http.StatusBadRequest)
		return
	}
	if err := validateNewPassword(req.NewPassword); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	// 改密只针对"登录者本人",即使超管处于 impersonate 视角也不允许
	// 替被视角用户改密(那是管理员重置密码的场景,走 /api/admin/users/{id}/reset-password)。
	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.logger.Error("get user failed", "user_id", scope.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(req.OldPassword)) != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "old password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		s.logger.Error("hash password failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hashStr := string(hash)
	clearFlag := false
	if err := s.st.UpdateUser(user.ID, store.UserUpdate{
		PassHash:           &hashStr,
		MustChangePassword: &clearFlag,
	}); err != nil {
		s.logger.Error("update password failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("password_change", clientIP(r), user.Username, "")

	// 吊销该用户全部会话(不止当前这条):凭证轮换后被窃会话不得继续有效。
	// 前端 axios 拦截器见 401 自动跳登录页。
	s.sessions.DestroyForUser(user.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, map[string]bool{"ok": true})
}

// requirePasswordChanged 拦截 must_change_password=true 的会话访问业务路由,
// 一律 403 + {must_change_password: true},前端 axios 拦截器据此跳改密页。
// 豁免面(/api/me、/api/me/password、/api/logout)在路由表表达:这些路由
// 只串 requireAuth,不经过本中间件(见 server.go Handler)。
// 必须串在 requireAuth 之后(依赖 context 里的 UserScope)。
func (s *Server) requirePasswordChanged(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := UserScopeFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// 会话载荷里的位以 users 表为准再查一次:管理员可能在会话存活期间
		// 重置了密码(ticket 05),只信会话会漏拦。读库失败降级放行,
		// 避免一次 DB 抖动把全员锁在门外。
		user, err := s.st.GetUserByID(scope.UserID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			s.logger.Warn("load user for must_change_password check failed, allowing", "user_id", scope.UserID, "error", err)
			next(w, r)
			return
		}
		if user.MustChangePassword {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{
				"must_change_password": true,
				"message":              "password change required",
			})
			return
		}
		next(w, r)
	}
}
