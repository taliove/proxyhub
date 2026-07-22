package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// topNodesMax 优质节点聚合接口的返回上限。前提是体检过的节点数 << 200
// (本地单用户场景,体检为手动/批量触发):截断按池遍历顺序而非分数,
// 超过该上限时池序靠后的高分节点不会返回(go-reviewer LOW-1 已知情接受)。
const topNodesMax = 200

// topNodeView 优质节点条目:最新体检 report + 自动标签 + 池内元信息。
// report 全量返回不裁剪:前端算分(calculateExamScore)与展示消费的字段横跨
// 全部四段(stability/region_speed/unlock/egress,见 web/src/components/exam/score.ts),
// 字段裁剪易漏且 report 本体不大,直接全返保持单一事实源。
type topNodeView struct {
	NodeKey   string               `json:"node_key"`
	Report    detection.ExamReport `json:"report"`
	Tags      []string             `json:"tags"`
	Type      string               `json:"type"` // 协议类型,前端分享操作门控(canGenerateShareLink)用
	Region    string               `json:"region"`
	Source    string               `json:"source"`
	Available bool                 `json:"available"`
}

// handleDashboardTopNodes 优质节点聚合:返回"体检过且当前在节点池"的节点清单。
// Go 侧不算加权总分(前端复用 calculateExamScore 排序取 Top 10,计分单一事实源);
// 不做分页,返回全量(上限 topNodesMax)。空结果返回 [] 而非 null。
func (s *Server) handleDashboardTopNodes(w http.ResponseWriter, r *http.Request) {
	pool := s.nodes.Nodes()
	keys := make([]string, 0, len(pool))
	for _, n := range pool {
		keys = append(keys, n.NodeKey())
	}

	// 以池内 node_key 为查询域取每节点最新 report(完整体检口径:排除"出网+稳定性"
	// 任务的缺段报告,总分聚合不被抢占):无体检记录的节点不出现在
	// 结果中,"已离池"的体检记录天然不在查询域内——两重过滤一次完成。
	reports, err := s.st.LatestCompleteExamReports(keys)
	if err != nil {
		s.logger.Error("get latest exam reports failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 标签查询失败降级为空(不阻塞主数据,同 handleListNodes 的降级约定)。
	nodeTags, err := s.st.ListNodeTags(keys)
	if err != nil {
		s.logger.Warn("get node tags failed", "error", err)
		nodeTags = nil
	}

	writeJSON(w, buildTopNodeViews(pool, reports, nodeTags))
}

// buildTopNodeViews 组装优质节点视图:按池顺序遍历保证输出稳定,
// 只保留有体检记录的节点;无标签节点的 tags 归一为空数组(序列化为 [] 而非 null)。
// 纯函数,不修改入参。
func buildTopNodeViews(
	pool []*subscription.Node,
	reports map[string]store.ExamHistoryEntry,
	nodeTags map[string][]string,
) []topNodeView {
	views := make([]topNodeView, 0, len(reports))
	for _, n := range pool {
		entry, ok := reports[n.NodeKey()]
		if !ok {
			continue
		}
		tags := nodeTags[n.NodeKey()]
		if tags == nil {
			tags = []string{}
		}
		views = append(views, topNodeView{
			NodeKey:   n.NodeKey(),
			Report:    entry.Report,
			Tags:      tags,
			Type:      n.Type,
			Region:    n.Region,
			Source:    n.Source,
			Available: n.Available,
		})
		if len(views) >= topNodesMax {
			break
		}
	}
	return views
}
