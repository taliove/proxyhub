package server

import (
	"github.com/taliove/proxyhub/internal/nodemon"
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
			picks := s.endpointNodePicks(ep)
			nodes := s.filteredNodesForMonitor(pool, uid, picks)
			nodes = s.applyConditions(nodes, ep)
			out = append(out, toMonitorTargets(uid, nodes)...)
		}
	}
	return out
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
