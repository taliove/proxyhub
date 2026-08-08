package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleListSelfNodes lists the current user's self-hosted nodes (including
// disabled) for the admin UI. ticket 07: per-user filter via EffectiveUserID.
func (s *Server) handleListSelfNodes(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodes, err := s.st.ListAllSelfHostedNodesByUser(EffectiveUserID(scope))
	if err != nil {
		s.logger.Error("list self nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"nodes": nodes})
}

// decodeSelfNode parses and validates self-hosted node request body (protocol + required fields)
func decodeSelfNode(r *http.Request) (*store.SelfHostedNode, error) {
	var n store.SelfHostedNode
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		return nil, errors.New("invalid request")
	}
	n.Name = strings.TrimSpace(n.Name)
	n.Server = strings.TrimSpace(n.Server)
	// Name can be empty: save fallback (applySelfNodeNameFallback) will auto-name by region or return 400.
	if n.Server == "" {
		return nil, errors.New("server is required")
	}
	if n.Port <= 0 || n.Port > 65535 {
		return nil, errors.New("invalid port")
	}
	switch n.Protocol {
	case "ss", "trojan", "vmess", "vless":
	default:
		return nil, errors.New("unsupported protocol")
	}
	return &n, nil
}

// handleCreateSelfNode creates a new self-hosted node (enabled by default,
// owned by the effective user, ticket 07)
func (s *Server) handleCreateSelfNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	n, err := decodeSelfNode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 身份查重(issue #53):同 server/port/protocol 的节点拒绝重复创建,
	// 从源头杜绝重复行(重复行会让展示/删除体验断裂)。
	if dup, dErr := s.st.SelfHostedNodeIdentityExists(EffectiveUserID(scope), n.Server, n.Port, n.Protocol, 0); dErr != nil {
		s.logger.Error("check self node identity failed", "error", dErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if dup {
		http.Error(w, "该节点已存在(相同服务器/端口/协议)", http.StatusConflict)
		return
	}
	n.Enabled = true
	n.RegionCode = s.resolveSelfNodeRegion(n)
	if err := applySelfNodeNameFallback(n); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.CreateSelfHostedNodeForUser(EffectiveUserID(scope), n); err != nil {
		// check-then-insert 竞态的 DB 兜底(023 唯一约束):与前置查重同款 409
		if errors.Is(err, store.ErrDuplicateIdentity) {
			http.Error(w, "该节点已存在(相同服务器/端口/协议)", http.StatusConflict)
			return
		}
		s.logger.Error("create self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	regionResolved := n.RegionCode != "" && n.RegionCode != "Unknown"
	writeJSON(w, map[string]any{"success": true, "region_resolved": regionResolved})
}

// handleUpdateSelfNode updates an existing self-hosted node (owner-checked,
// ticket 07)
func (s *Server) handleUpdateSelfNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	n, err := decodeSelfNode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.ID = id
	// 身份查重(issue #53):编辑改成与他人重复的身份同样拒绝,排除自身。
	if dup, dErr := s.st.SelfHostedNodeIdentityExists(EffectiveUserID(scope), n.Server, n.Port, n.Protocol, id); dErr != nil {
		s.logger.Error("check self node identity failed", "error", dErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if dup {
		http.Error(w, "该节点已存在(相同服务器/端口/协议)", http.StatusConflict)
		return
	}
	n.RegionCode = s.resolveSelfNodeRegion(n)
	if err := applySelfNodeNameFallback(n); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.UpdateSelfHostedNodeForUser(EffectiveUserID(scope), n); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// check-then-update 竞态的 DB 兜底(023 唯一约束):与前置查重同款 409
		if errors.Is(err, store.ErrDuplicateIdentity) {
			http.Error(w, "该节点已存在(相同服务器/端口/协议)", http.StatusConflict)
			return
		}
		s.logger.Error("update self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Sync memory pool so /nodes reflects the edited name/region immediately
	// (ticket 47): no waiting for the next aggregation refresh.
	s.syncSelfHostedNodeIdentityForUser(EffectiveUserID(scope), n.ToNode().NodeKey(), n.Name, n.RegionCode)
	regionResolved := n.RegionCode != "" && n.RegionCode != "Unknown"
	writeJSON(w, map[string]any{"success": true, "region_resolved": regionResolved})
}

// handleDeleteSelfNode deletes a self-hosted node (owner-checked, ticket 07)
func (s *Server) handleDeleteSelfNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.st.DeleteSelfHostedNodeForUser(EffectiveUserID(scope), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleToggleSelfNode enables/disables a self-hosted node (owner-checked,
// ticket 07)
func (s *Server) handleToggleSelfNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := s.st.SetSelfHostedNodeEnabledForUser(EffectiveUserID(scope), id, req.Enabled); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("toggle self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleBlockNode 屏蔽机场节点（按 NodeKey）。写请求者本人名单(多租户)。
func (s *Server) handleBlockNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	key, err := decodeNodeKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.BlockNodeForUser(EffectiveUserID(scope), key); err != nil {
		s.logger.Error("block node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleUnblockNode 取消屏蔽机场节点(只动请求者本人名单)。
func (s *Server) handleUnblockNode(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	key, err := decodeNodeKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.UnblockNodeForUser(EffectiveUserID(scope), key); err != nil {
		s.logger.Error("unblock node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// batchBlockRequest 批量屏蔽/取消屏蔽请求体。
// 二选一：显式给出 node_keys，或给出 source（机场名，按当前节点池展开为该机场全部 NodeKey）。
type batchBlockRequest struct {
	NodeKeys []string `json:"node_keys"`
	Source   string   `json:"source"`
}

// resolveBatchKeys extracts target NodeKey list from request.
// Prefers explicit node_keys; otherwise collects all NodeKeys from the caller's
// own pool shard (multi-tenant) for the given airport source
// (self-hosted nodes are exempt).
func (s *Server) resolveBatchKeys(req batchBlockRequest, userID int64) []string {
	if len(req.NodeKeys) > 0 {
		return req.NodeKeys
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		return nil
	}
	seen := make(map[string]bool)
	var keys []string
	for _, n := range s.nodes.NodesForUser(userID) {
		if n.Source != source || n.Source == subscription.SourceSelfHosted {
			continue
		}
		key := n.NodeKey()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// handleBatchBlockNodes 批量屏蔽：按机场（source）或显式 node_keys 一次性拉黑，跨刷新持久。
// 写请求者本人名单(多租户)。
func (s *Server) handleBatchBlockNodes(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	var req batchBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	effUID := EffectiveUserID(scope)
	keys := s.resolveBatchKeys(req, effUID)
	if len(keys) == 0 {
		http.Error(w, "no matching nodes: provide node_keys or a valid source", http.StatusBadRequest)
		return
	}
	if err := s.st.BlockNodesForUser(effUID, keys); err != nil {
		s.logger.Error("batch block nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true, "count": len(keys)})
}

// handleBatchUnblockNodes 批量取消屏蔽：按机场（source）或显式 node_keys。
// 只动请求者本人名单(多租户)。
func (s *Server) handleBatchUnblockNodes(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	var req batchBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	effUID := EffectiveUserID(scope)
	keys := s.resolveBatchKeys(req, effUID)
	if len(keys) == 0 {
		http.Error(w, "no matching nodes: provide node_keys or a valid source", http.StatusBadRequest)
		return
	}
	if err := s.st.UnblockNodesForUser(effUID, keys); err != nil {
		s.logger.Error("batch unblock nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true, "count": len(keys)})
}

// decodeNodeKey 从请求体解析 node_key（server:port）
func decodeNodeKey(r *http.Request) (string, error) {
	var req struct {
		NodeKey string `json:"node_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", errors.New("invalid request")
	}
	key := strings.TrimSpace(req.NodeKey)
	if key == "" {
		return "", errors.New("node_key is required")
	}
	return key, nil
}

// resolveSelfNodeRegion resolves self-hosted node region code with real dependencies (offline DB, failure does not block save).
// Region lookup uses embedded GeoIP (geoip.LookupCountry), no longer calls ip-api.com.
func (s *Server) resolveSelfNodeRegion(n *store.SelfHostedNode) string {
	recognizer, err := s.st.NewRegionRecognizer()
	if err != nil {
		s.logger.Warn("build region recognizer failed", "error", err)
		return "Unknown"
	}
	deps := regionResolverDeps{
		lookupHost:    s.lookupHost,
		countryLookup: s.countryLookup,
		recognize:     recognizer.Recognize,
	}
	return resolveRegionCode(n.Server, n.Name, deps)
}

// selfNodeNamePrefix is the auto-naming prefix for self-hosted nodes, forming "自建{region Chinese name}" (e.g. 自建香港).
const selfNodeNamePrefix = "自建"

// suggestSelfNodeName uses offline GeoIP to derive suggested name and region code for server: pure geo chain
// (IP direct or DNS resolve first IP -> LookupCountry -> CountryName), no name recognition fallback.
// Any step failure (empty server/DNS failure/no country record/missing Chinese name) returns empty strings, letting caller silently degrade.
func (s *Server) suggestSelfNodeName(server string) (name, regionCode string) {
	if server == "" {
		return "", ""
	}
	deps := regionResolverDeps{
		lookupHost:    s.lookupHost,
		countryLookup: s.countryLookup,
	}
	code := resolveRegionGeoOnly(server, deps)
	if code == "" {
		return "", ""
	}
	cn := geoip.CountryName(code)
	if cn == "" {
		return "", ""
	}
	return selfNodeNamePrefix + cn, code
}

// handleSuggestSelfNode provides self-hosted node naming suggestion: input server (IP/domain) walks pure offline GeoIP chain
// to reverse-lookup region, returning {"name":"自建香港","regionCode":"HK"} on hit. Any step failure uniformly returns
// 200 + empty fields (both name/regionCode empty), letting frontend silently degrade to not fill, avoiding 404.
func (s *Server) handleSuggestSelfNode(w http.ResponseWriter, r *http.Request) {
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	name, code := s.suggestSelfNodeName(server)
	writeJSON(w, map[string]string{"name": name, "regionCode": code})
}

// applySelfNodeNameFallback is the save fallback: when name (after trim) is empty, if the backend-resolved region code
// has a corresponding Chinese name, auto-name as "自建{Chinese name}" for DB; region resolution failure (Unknown/empty or missing Chinese name)
// returns name-required error (caller responds 400). User-provided name is left unchanged.
// Requires n.RegionCode to be already resolved as authoritative value by resolveSelfNodeRegion.
func applySelfNodeNameFallback(n *store.SelfHostedNode) error {
	if strings.TrimSpace(n.Name) != "" {
		return nil
	}
	cn := geoip.CountryName(n.RegionCode)
	if cn == "" {
		return errors.New("name is required")
	}
	n.Name = selfNodeNamePrefix + cn
	return nil
}
