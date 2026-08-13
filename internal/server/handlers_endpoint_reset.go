package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
)

// 订阅链接重置(issue #117)的三个 HTTP 入口:
// 用户自助重置、用户自助延长宽限、管理员代为重置。
// 共用同一 store 方法;宽限语义与审计约定见 endpoint_reset.go。

// handleResetEndpointLink 用户自助重置:原位轮换 path+token,端点配置全保留,
// 旧链接开启 3 天宽限。行属他人 404(与端点不存在无差别)。
func (s *Server) handleResetEndpointLink(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.ResetEndpointLinkForUser(EffectiveUserID(scope), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("reset endpoint link failed", "endpoint_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.logger.Info("endpoint link reset",
		"operator_user_id", EffectiveUserID(scope), "endpoint_id", ep.ID, "endpoint_owner", ep.UserID)
	writeJSON(w, ep)
}

// handleExtendEndpointGrace 延长宽限 +3 天(仅宽限存活期;过期/从未重置
// 与端点不存在同 404,不可复活)。
func (s *Server) handleExtendEndpointGrace(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.ExtendEndpointGraceForUser(EffectiveUserID(scope), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("extend endpoint grace failed", "endpoint_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.logger.Info("endpoint grace extended",
		"operator_user_id", EffectiveUserID(scope), "endpoint_id", ep.ID)
	writeJSON(w, ep)
}

// handleAdminListUserEndpoints 列出指定用户的订阅地址(adminGuard 之后),
// 供管理端"代为重置"界面逐条操作。只读,不改任何状态。
func (s *Server) handleAdminListUserEndpoints(w http.ResponseWriter, r *http.Request) {
	uid, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	eps, err := s.st.ListEndpointsByUser(uid)
	if err != nil {
		s.logger.Error("admin list user endpoints failed", "user_id", uid, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if eps == nil {
		eps = []*store.Endpoint{}
	}
	writeJSON(w, eps)
}

// handleAdminResetEndpointLink 管理员代为重置指定用户的订阅链接(adminGuard 之后):
// 原位轮换;行不属该用户或轮换失败 ErrNotFound 时 404(与端点不存在无差别)。
// 用户失联/链接泄露应急用。
func (s *Server) handleAdminResetEndpointLink(w http.ResponseWriter, r *http.Request) {
	uid, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid endpoint id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.ResetEndpointLinkForUser(uid, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("admin reset endpoint link failed", "endpoint_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	operatorID, _ := UserScopeFromContext(r.Context())
	s.logger.Info("endpoint link reset by admin",
		"operator_admin_id", operatorID.UserID, "target_user_id", uid, "endpoint_id", ep.ID)
	writeJSON(w, ep)
}
