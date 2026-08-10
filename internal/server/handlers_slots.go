package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 名称槽位 HTTP API(ADR 0047 / issue #97)。全部按请求者用户空间隔离(多租户)。

// slotNodeView 槽位挂载节点的摘要(在线状态供管理界面亮灯)
type slotNodeView struct {
	Name      string `json:"name"`       // 机场原名
	Source    string `json:"source"`     // 来源机场
	Region    string `json:"region"`     // 地区码
	Available bool   `json:"available"`  // 最近检测可用性
	Latency   int    `json:"latency"`    // 延迟(ms)
	Stale     bool   `json:"stale"`      // 已从机场订阅消失
	Missing   bool   `json:"missing"`    // 池里找不到(键已孤儿化)
	// 最近一次监控探测(issue #103):无监控数据时为空
	LastProbeAt string `json:"last_probe_at,omitempty"`
	LastProbeOK bool   `json:"last_probe_ok"`
}

// slotView 槽位对外视图
type slotView struct {
	Name      string        `json:"name"`
	NodeKey   string        `json:"node_key"`
	Empty     bool          `json:"empty"` // 空槽(未指派节点)
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	Node      *slotNodeView `json:"node,omitempty"`
}

// poolNodeIndex 建 node_key → 池节点索引(含 stale,供槽位摘要判 missing/stale)
func (s *Server) poolNodeIndex(userID int64) map[string]*subscription.Node {
	idx := make(map[string]*subscription.Node)
	for _, n := range s.nodes.NodesForUser(userID) {
		// 同 key 多节点(跨机场共存)取首个,摘要场景足够
		if _, ok := idx[n.NodeKey()]; !ok {
			idx[n.NodeKey()] = n
		}
	}
	return idx
}

// handleListSlots GET /api/slots:我的槽位列表 + 迁移冲突待办。
func (s *Server) handleListSlots(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)

	slots, err := s.st.ListNameSlotsForUser(effUID)
	if err != nil {
		s.logger.Error("list slots failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	conflicts, err := s.st.ListSlotMigrationConflictsForUser(effUID)
	if err != nil {
		s.logger.Warn("list slot migration conflicts failed", "error", err)
		conflicts = nil // 降级:冲突待办不阻塞主列表
	}

	idx := s.poolNodeIndex(effUID)
	views := make([]slotView, 0, len(slots))
	for _, sl := range slots {
		v := slotView{
			Name:      sl.Name,
			NodeKey:   sl.NodeKey,
			Empty:     sl.NodeKey == "",
			CreatedAt: sl.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: sl.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if sl.NodeKey != "" {
			if n, ok := idx[sl.NodeKey]; ok {
				v.Node = &slotNodeView{
					Name: n.Name, Source: n.Source, Region: n.Region,
					Available: n.Available, Latency: n.Latency, Stale: n.Stale,
				}
			} else {
				v.Node = &slotNodeView{Missing: true}
			}
			// 最近一次监控探测打点(节点可能已消失,探测数据仍可展示)
			if samples, serr := s.st.ListMonitorSamples(sl.NodeKey, 1); serr == nil && len(samples) > 0 {
				if v.Node == nil {
					v.Node = &slotNodeView{Missing: true}
				}
				v.Node.LastProbeAt = samples[0].CheckedAt.Local().Format("2006-01-02 15:04:05")
				v.Node.LastProbeOK = samples[0].OK
			} else if serr != nil {
				s.logger.Warn("list monitor samples failed", "key", sl.NodeKey, "error", serr)
			}
		}
		views = append(views, v)
	}

	writeJSON(w, map[string]any{
		"slots":     views,
		"conflicts": conflicts,
	})
}

// slotConflictPayload 409 结构化载荷:前端据此弹确认("该节点当前叫 X,确认转移?")
func slotConflictPayload(ce *store.SlotConflictError) map[string]any {
	return map[string]any{
		"error": "slot conflict",
		"conflict": map[string]any{
			"kind":            string(ce.Kind),
			"name":            ce.Name,
			"node_key":        ce.NodeKey,
			"holder_name":     ce.HolderName,
			"holder_node_key": ce.HolderNodeKey,
		},
	}
}

// writeSlotError 槽位错误的统一状态码映射。并发下 DB 唯一约束兜底冒出的裸
// sqlite UNIQUE 错误在此翻译成通用 409(见 name_slots.go TODO,kind=unknown)。
func writeSlotError(s *Server, w http.ResponseWriter, err error) {
	var ce *store.SlotConflictError
	switch {
	case errors.As(err, &ce):
		writeJSONStatus(w, http.StatusConflict, slotConflictPayload(ce))
	case errors.Is(err, store.ErrSlotNotFound):
		http.Error(w, "slot not found", http.StatusNotFound)
	case errors.Is(err, store.ErrSlotNameEmpty):
		http.Error(w, "name required", http.StatusBadRequest)
	case strings.Contains(err.Error(), "UNIQUE constraint failed: name_slots"):
		s.logger.Warn("slot unique constraint hit (concurrent write)", "error", err)
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error":    "slot conflict",
			"conflict": map[string]any{"kind": "concurrent"},
		})
	default:
		s.logger.Error("slot operation failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleCreateSlot POST /api/slots:新建槽位 {name, node_key?, force?};
// node_key 空 = 预建空槽;非空时须存在于本人池(防手误指向幽灵节点)。
func (s *Server) handleCreateSlot(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)

	var req struct {
		Name    string `json:"name"`
		NodeKey string `json:"node_key"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Name = store.SanitizeSlotName(req.Name)
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.NodeKey != "" {
		if _, ok := s.poolNodeIndex(effUID)[req.NodeKey]; !ok {
			http.Error(w, "unknown node_key", http.StatusBadRequest)
			return
		}
	}

	if err := s.st.CreateNameSlotForUser(effUID, req.Name, req.NodeKey, req.Force); err != nil {
		writeSlotError(s, w, err)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// handleUpdateSlot PUT /api/slots/{name}:改名/指派/转移/摘下。
// {new_name?, node_key?, force?};new_name/node_key 缺省=不变,node_key 显式空串=摘下变空槽。
func (s *Server) handleUpdateSlot(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	name := r.PathValue("name")

	var req struct {
		NewName *string `json:"new_name"`
		NodeKey *string `json:"node_key"`
		Force   bool    `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	newName := ""
	if req.NewName != nil {
		newName = store.SanitizeSlotName(*req.NewName)
		if newName == "" {
			http.Error(w, "new_name required", http.StatusBadRequest)
			return
		}
	}
	nodeKey := ""
	if req.NodeKey != nil {
		nodeKey = *req.NodeKey
		if nodeKey != "" {
			if _, ok := s.poolNodeIndex(effUID)[nodeKey]; !ok {
				http.Error(w, "unknown node_key", http.StatusBadRequest)
				return
			}
		}
	}

	// 摘下(node_key 显式空串)与改名/指派拆开执行,store 层语义保持单一
	if req.NodeKey != nil && nodeKey == "" {
		if req.NewName != nil && newName != name {
			if err := s.st.UpdateNameSlotForUser(effUID, name, newName, "", req.Force); err != nil {
				writeSlotError(s, w, err)
				return
			}
			name = newName
		}
		if err := s.st.UnassignNameSlotForUser(effUID, name); err != nil {
			writeSlotError(s, w, err)
			return
		}
		writeJSON(w, map[string]any{"success": true})
		return
	}

	if err := s.st.UpdateNameSlotForUser(effUID, name, newName, nodeKey, req.Force); err != nil {
		writeSlotError(s, w, err)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// handleDeleteSlot DELETE /api/slots/{name}:删除槽位,节点回退模板/原名。
func (s *Server) handleDeleteSlot(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteNameSlotForUser(EffectiveUserID(scope), r.PathValue("name")); err != nil {
		writeSlotError(s, w, err)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}
