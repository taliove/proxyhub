// Package subfilter 实现订阅地址的「节点范围」筛选条件(动态查询)。
//
// 每个订阅地址可配置一组结构化筛选条件(Conditions),拉取订阅时按条件从当前
// 节点池动态过滤出该地址的节点范围。条件为空=全量(现状行为,零回归)。因为在
// 拉取那一刻求值而非落静态清单,机场刷新后订阅自动跟随,永不腐化(见设计文档
// design-subscription-conditions.md)。
//
// # 谓词语义(与前端 web/src/views/nodes/predicates.ts 的对齐契约)
//
// 求值是纯函数,对每个节点独立判定,四个维度全部满足才命中(跨维度 AND):
//
//   - Airports:机场名集合,匹配 node.Source。非空时 node.Source 必须 ∈ 集合(维度内 OR)。
//   - Regions :地区码集合,匹配 node.Region(如 HK/US/JP,recognizer 识别的码,非国家出口码)。
//     非空时 node.Region 必须 ∈ 集合(维度内 OR)。
//   - Tags    :自动标签集合(node_tags 表,含解锁 nf-full/openai、稳定 stable-good、
//     出网 fast/ipv6/region:US 等)。非空时节点标签集必须 ⊇ 条件集(维度内 AND:全含才命中)。
//   - Keyword :对 node.Name 做大小写不敏感子串匹配;空白视为无此条件。
//
// 维度内为何 OR/AND 不同:一个节点只有单一 Source/Region,多选机场/地区只能是"任一"(OR);
// 一个节点可带多个标签,多选标签取"全含"(AND)才能表达"US 且 Netflix 且稳定"这类交集意图。
//
// 匹配大小写:Keyword 折叠大小写(子串);Airports/Regions/Tags 精确匹配(选项来自真实
// 枚举值下拉,精确即可预测,与节点表格精确筛选一致)。
package subfilter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Conditions 订阅地址的节点范围筛选条件(JSON 存 endpoints.conditions 列)。
// 零值(全空)表示不筛选。切片省略序列化(omitempty),旧端点默认空。
type Conditions struct {
	Airports []string `json:"airports,omitempty"` // 机场名(node.Source),维度内 OR
	Regions  []string `json:"regions,omitempty"`  // 地区码(node.Region),维度内 OR
	Tags     []string `json:"tags,omitempty"`     // 自动标签(node_tags),维度内 AND(全含)
	Keyword  string   `json:"keyword,omitempty"`  // 节点名子串,大小写不敏感
}

// Parse 解析 endpoints.conditions 列的原始 JSON。空串(旧端点默认)解析为空条件。
// 非法 JSON 返回错误,由调用方在边界处 fail fast。
func Parse(raw string) (Conditions, error) {
	var c Conditions
	if strings.TrimSpace(raw) == "" {
		return c, nil
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Conditions{}, fmt.Errorf("parse subscription conditions: %w", err)
	}
	return c, nil
}

// Marshal 序列化为规范 JSON 字符串(供落库)。空条件返回 "{}"。
func (c Conditions) Marshal() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal subscription conditions: %w", err)
	}
	return string(b), nil
}

// IsEmpty 报告条件是否为空(无任何有效维度)。空条件=全量,过滤直接短路返回原池。
func (c Conditions) IsEmpty() bool {
	return len(c.Airports) == 0 && len(c.Regions) == 0 && len(c.Tags) == 0 &&
		strings.TrimSpace(c.Keyword) == ""
}

// NeedsTags 报告条件是否引用标签维度(供调用方决定是否需要拉取 node_tags,
// 无 tag 条件时可跳过一次 DB 查询)。
func (c Conditions) NeedsTags() bool {
	return len(c.Tags) > 0
}

// Match 报告单个节点(连同其标签集)是否满足全部条件(跨维度 AND)。
// nodeTags 为该节点在 node_tags 中的标签(可为 nil/空,tag 条件即不命中)。
func (c Conditions) Match(n *subscription.Node, nodeTags []string) bool {
	if !matchAny(c.Airports, n.Source) {
		return false
	}
	if !matchAny(c.Regions, n.Region) {
		return false
	}
	if !matchAllTags(c.Tags, nodeTags) {
		return false
	}
	if kw := strings.TrimSpace(c.Keyword); kw != "" {
		if !strings.Contains(strings.ToLower(n.Name), strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

// Filter 返回节点池中满足条件的子集(维持原顺序,不修改入参节点)。
// tagsByKey 为 NodeKey -> 标签集(见 store.ListNodeTags);缺失键按空标签处理。
// 空条件短路返回原池的浅拷贝(零回归)。始终返回非 nil 切片。
func Filter(nodes []*subscription.Node, tagsByKey map[string][]string, c Conditions) []*subscription.Node {
	out := make([]*subscription.Node, 0, len(nodes))
	if c.IsEmpty() {
		return append(out, nodes...)
	}
	for _, n := range nodes {
		if c.Match(n, tagsByKey[n.NodeKey()]) {
			out = append(out, n)
		}
	}
	return out
}

// matchAny 报告 value 是否命中集合(集合为空=无此维度约束,恒真;维度内 OR)。
func matchAny(set []string, value string) bool {
	if len(set) == 0 {
		return true
	}
	for _, s := range set {
		if s == value {
			return true
		}
	}
	return false
}

// matchAllTags 报告节点标签集是否包含全部所需标签(want 为空=无约束,恒真;维度内 AND)。
func matchAllTags(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	if len(have) == 0 {
		return false
	}
	haveSet := make(map[string]struct{}, len(have))
	for _, t := range have {
		haveSet[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := haveSet[t]; !ok {
			return false
		}
	}
	return true
}
