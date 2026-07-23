package server

import (
	"sort"
	"strings"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/subscription"
)

// DefaultPageSize 节点列表默认每页条数(未指定或非法时使用)。
const DefaultPageSize = 20

// NodeQuery 节点分页查询参数(见 ADR 0013)。零值字段表示不筛选。
type NodeQuery struct {
	// 筛选
	Region    string // 地区代码,精确匹配
	Type      string // 节点类型,精确匹配
	Available *bool  // nil=不筛选
	Blocked   *bool  // nil=不筛选
	Stale     *bool  // nil=不筛选(true=仅已下架,false=仅在架)
	Source    string // 机场名,子串模糊匹配
	Keyword   string // 关键词:名称子串(大小写不敏感)或地区(码精确/中文名,见 matchesKeyword)

	// 排序
	SortBy    string // latency / name / region / source,默认 latency
	SortOrder string // asc / desc,默认 asc

	// 分页
	Page     int // 1 起
	PageSize int
}

// NodeQueryResult 分页查询结果。
type NodeQueryResult struct {
	Nodes      []*subscription.Node
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// QueryNodes 对内存节点池执行筛选 → 排序 → 分页(见 ADR 0013)。
//
// 在内存池上操作(而非 SQL),与订阅生成同源,保证后台所见即所得:
// 池含自建节点、反映最近一次刷新,DB 的 nodes 表只是重启回填快照。
// blocked 为屏蔽名单(NodeKey→true),用于 Blocked 筛选与结果标记。
func QueryNodes(nodes []*subscription.Node, blocked map[string]bool, q NodeQuery) NodeQueryResult {
	filtered := filterNodes(nodes, blocked, q)
	sortNodes(filtered, q.SortBy, q.SortOrder)
	return paginate(filtered, q.Page, q.PageSize)
}

// filterNodes 按查询条件过滤,返回新切片。
func filterNodes(nodes []*subscription.Node, blocked map[string]bool, q NodeQuery) []*subscription.Node {
	out := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		if q.Region != "" && n.Region != q.Region {
			continue
		}
		if q.Type != "" && n.Type != q.Type {
			continue
		}
		if q.Available != nil && n.Available != *q.Available {
			continue
		}
		if q.Stale != nil && n.Stale != *q.Stale {
			continue
		}
		if q.Source != "" && !strings.Contains(n.Source, q.Source) {
			continue
		}
		if q.Keyword != "" && !matchesKeyword(n, q.Keyword) {
			continue
		}
		if q.Blocked != nil {
			isBlocked := blocked[n.NodeKey()]
			if isBlocked != *q.Blocked {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// matchesKeyword 关键词匹配(见 ticket 0041),任一命中即保留:
//  1. 名称/标准化展示名包含关键词(大小写不敏感子串);
//  2. 地区码与关键词精确相等(不区分大小写,如 "jp" 命中 Region=JP);
//  3. 地区码经 geoip.CountryName 映射的中文名包含关键词(如 "日本" 命中 Region=JP)。
//
// 三者皆不命中则过滤掉;关键词既不像名称片段也不像地区时结果为空,属合法。
// 注意:web/src/views/nodes/predicates.ts 另有一套客户端 matchesKeyword
// (名称/标准名/服务器,服务节点管理页),字段集有意不同——本套多地区两路、少服务器,
// 改任一处的字段集时对照另一处确认语义分叉是否合理。
func matchesKeyword(n *subscription.Node, keyword string) bool {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return true
	}
	lower := strings.ToLower(kw)
	if strings.Contains(strings.ToLower(n.Name), lower) {
		return true
	}
	if n.DisplayName != "" && strings.Contains(strings.ToLower(n.DisplayName), lower) {
		return true
	}
	if n.Region == "" {
		return false
	}
	if strings.EqualFold(n.Region, kw) {
		return true
	}
	if cn := geoip.CountryName(n.Region); cn != "" && strings.Contains(cn, kw) {
		return true
	}
	return false
}

// sortNodes 原地排序。默认 latency asc。使用稳定排序,NodeKey 作次级键保证确定性。
func sortNodes(nodes []*subscription.Node, sortBy, order string) {
	desc := order == "desc"
	less := func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		var cmp int
		switch sortBy {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "region":
			cmp = strings.Compare(a.Region, b.Region)
		case "source":
			cmp = strings.Compare(a.Source, b.Source)
		default: // latency
			cmp = a.Latency - b.Latency
		}
		if cmp == 0 {
			cmp = strings.Compare(a.NodeKey(), b.NodeKey()) // 稳定次级键
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	}
	sort.SliceStable(nodes, less)
}

// paginate 截取指定页。Page/PageSize 非正时归一到安全默认值;
// 超出末页的 Page 返回空 Nodes 并原样回显请求页码(前端据 TotalPages 自行纠偏)。
func paginate(nodes []*subscription.Node, page, pageSize int) NodeQueryResult {
	total := len(nodes)
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if page <= 0 {
		page = 1
	}
	totalPages := (total + pageSize - 1) / pageSize

	start := (page - 1) * pageSize
	end := start + pageSize
	var pageNodes []*subscription.Node
	if start < total {
		if end > total {
			end = total
		}
		pageNodes = nodes[start:end]
	} else {
		pageNodes = []*subscription.Node{}
	}

	return NodeQueryResult{
		Nodes:      pageNodes,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}
