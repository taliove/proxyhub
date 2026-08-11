package server

import (
	"errors"

	"github.com/taliove/proxyhub/internal/nodemon"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// MonitorTargets 实现 nodemon.TargetProvider(ADR 0047 / issue #99):
// 监控集合 = 每个用户各订阅地址"实际下发节点"的并集(跨用户由 nodemon
// 按 node_key 物理去重)。口径与下发链同源,但排除可用性/延迟过滤——
// 否则节点一判宕就掉出集合,永远等不到恢复探测(见 filteredNodesForMonitor)。
// 禁用地址不下发,也不监控。
func (s *Server) MonitorTargets() []nodemon.Target {
	users, err := s.st.ListUsers()
	if err != nil {
		s.logger.Warn("list users for monitor failed", "error", err)
		return nil
	}

	var out []nodemon.Target
	// 追加 userID=0 未归属桶(历史数据/单管理员场景,见 node.UserID 注释)
	userIDs := make([]int64, 0, len(users)+1)
	userIDs = append(userIDs, 0)
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}

	for _, uid := range userIDs {
		eps, err := s.st.ListEndpointsByUser(uid)
		if err != nil {
			s.logger.Warn("list endpoints for monitor failed", "user_id", uid, "error", err)
			continue
		}
		if len(eps) == 0 {
			continue
		}
		// 每个地址的实发集合独立计算(精选/条件各异),再并入用户级并集
		pool := s.nodes.NodesForUser(uid)
		for _, ep := range eps {
			if !ep.Enabled {
				continue
			}
			if ep.SlotMode {
				// 槽位模式:监控集合 = 槽位挂载节点(池过滤链不参与,见 slotModeDeliverable)
				out = append(out, toMonitorTargets(uid, s.slotModeMonitorNodes(uid))...)
				continue
			}
			picks := s.endpointNodePicks(ep)
			nodes := s.filteredNodesForMonitor(pool, uid, picks)
			nodes = s.applyConditions(nodes, ep)
			out = append(out, toMonitorTargets(uid, nodes)...)
		}
	}
	return out
}

// slotModeMonitorNodes 槽位模式的监控集合:该用户槽位挂载的节点。
// stale/屏蔽不监控(已消失/已拉黑没有探测意义);可用性不排除
// (宕机节点必须在监控中,否则等不到恢复探测)。
func (s *Server) slotModeMonitorNodes(userID int64) []*subscription.Node {
	slots, err := s.st.SlotNameByNodeKeyForUser(userID)
	if err != nil || len(slots) == 0 {
		return nil
	}
	pool := s.mergeSelfHosted(s.nodes.NodesForUser(userID), userID)
	var nodes []*subscription.Node
	for _, n := range pool {
		if _, ok := slots[n.NodeKey()]; ok {
			nodes = append(nodes, n)
		}
	}
	nodes = filterStaleNodes(nodes)
	if blocked, berr := s.st.ListBlockedNodesForUser(userID); berr == nil {
		nodes = filterBlockedNodes(nodes, blocked)
	}
	return nodes
}

func toMonitorTargets(userID int64, nodes []*subscription.Node) []nodemon.Target {
	out := make([]nodemon.Target, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodemon.Target{
			UserID:  userID,
			NodeKey: n.NodeKey(),
			Server:  n.Server,
			Port:    n.Port,
		})
	}
	return out
}

// monitorImmuneKeys 宕机免疫集合(ADR 0047 / issue #101):监控开启时返回该用户
// 全部启用地址的监控集合并集(与 MonitorTargets 同口径);监控关闭或读失败
// 返回 nil——过滤链走原始可用性过滤(零回归)。
func (s *Server) monitorImmuneKeys(userID int64) map[string]bool {
	v, err := s.st.GetSetting("subscription_monitor_enabled")
	if err != nil {
		// ErrNotFound = 未配置(功能默认关),是常态不告警;只记录真实 DB 故障
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("read subscription_monitor_enabled failed, immunity off", "error", err)
		}
		return nil
	}
	if v != "true" {
		return nil
	}
	eps, err := s.st.ListEndpointsByUser(userID)
	if err != nil {
		s.logger.Warn("list endpoints for immune keys failed", "user_id", userID, "error", err)
		return nil
	}
	pool := s.nodes.NodesForUser(userID)
	keys := make(map[string]bool)
	slotModeAdded := false
	for _, ep := range eps {
		if !ep.Enabled {
			continue
		}
		if ep.SlotMode {
			// 槽位模式地址的免疫集 = 槽位挂载节点(只算一次,跨地址同集合)
			if !slotModeAdded {
				for _, n := range s.slotModeMonitorNodes(userID) {
					keys[n.NodeKey()] = true
				}
				slotModeAdded = true
			}
			continue
		}
		nodes := s.filteredNodesForMonitor(pool, userID, s.endpointNodePicks(ep))
		nodes = s.applyConditions(nodes, ep)
		for _, n := range nodes {
			keys[n.NodeKey()] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}
