package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// topNodesFixtureNode 构造池内节点 fixture:全零 UUID + example.com,绝不含真实凭证。
func topNodesFixtureNode(name, server string, port int, region, source string, available bool) *subscription.Node {
	return &subscription.Node{
		Name:      name,
		Server:    server,
		Port:      port,
		Type:      "vmess",
		UUID:      "00000000-0000-0000-0000-000000000000",
		Region:    region,
		Source:    source,
		Available: available,
	}
}

// topNodesExamSeed 一个节点的体检历史种子(reports 按写入顺序,最后一条为最新)。
type topNodesExamSeed struct {
	nodeKey string
	reports []detection.ExamReport
}

// stabilityOnlyReport 构造只含稳定性段的体检报告(fixture:不含任何凭证字段)。
func stabilityOnlyReport(score int) detection.ExamReport {
	return detection.ExamReport{
		Stability: &detection.StabilityMetrics{Total: 10, Succeeded: 10, Score: score},
	}
}

// topNodeResponseItem 接口响应条目(与生产结构同形,独立定义避免测试依赖内部类型名)。
type topNodeResponseItem struct {
	NodeKey   string               `json:"node_key"`
	Report    detection.ExamReport `json:"report"`
	Tags      []string             `json:"tags"`
	Type      string               `json:"type"`
	Region    string               `json:"region"`
	Source    string               `json:"source"`
	Available bool                 `json:"available"`
}

// TestHandleDashboardTopNodes 优质节点聚合接口:只返回"体检过且当前在池"的节点,
// 每节点取最新一条 report,关联标签,补池内 type/region/source/available。
func TestHandleDashboardTopNodes(t *testing.T) {
	poolA := topNodesFixtureNode("hk-01", "a.example.com", 443, "香港", "airport-a", true)
	poolB := topNodesFixtureNode("jp-01", "b.example.com", 443, "日本", "airport-b", false)
	poolC := topNodesFixtureNode("us-01", "c.example.com", 443, "美国", "airport-a", true)
	offPoolKey := "gone.example.com:443"

	cases := []struct {
		name string
		pool []*subscription.Node
		// exams 体检历史种子;nodeKey 可不在池中(模拟体检后离池)。
		exams []topNodesExamSeed
		// tags 写入 node_tags 的标签。
		tags map[string][]string
		// wantKeys 期望出现的 node_key(顺序 = 池顺序)。
		wantKeys []string
		// wantScores 期望每节点取到最新一条 report 的稳定性分。
		wantScores map[string]int
		// wantRawBody 非空时直接断言整个 body(用于 "[]" 空态)。
		wantRawBody string
		// wantTagsEmptyArray 非空时断言该节点的 tags 序列化为 [] 而非 null。
		wantTagsEmptyArray string
	}{
		{
			name:        "空池空库返回空数组而非null",
			pool:        nil,
			wantRawBody: "[]",
		},
		{
			name:        "池内节点均无体检记录返回空数组",
			pool:        []*subscription.Node{poolA, poolB},
			wantRawBody: "[]",
		},
		{
			name: "无体检记录的节点不出现",
			pool: []*subscription.Node{poolA, poolB},
			exams: []topNodesExamSeed{
				{nodeKey: poolA.NodeKey(), reports: []detection.ExamReport{stabilityOnlyReport(90)}},
			},
			wantKeys:   []string{poolA.NodeKey()},
			wantScores: map[string]int{poolA.NodeKey(): 90},
		},
		{
			name: "体检过但已离池的节点被过滤",
			pool: []*subscription.Node{poolA},
			exams: []topNodesExamSeed{
				{nodeKey: poolA.NodeKey(), reports: []detection.ExamReport{stabilityOnlyReport(90)}},
				{nodeKey: offPoolKey, reports: []detection.ExamReport{stabilityOnlyReport(100)}},
			},
			wantKeys:   []string{poolA.NodeKey()},
			wantScores: map[string]int{poolA.NodeKey(): 90},
		},
		{
			name: "同一节点多条历史取最新一条",
			pool: []*subscription.Node{poolA},
			exams: []topNodesExamSeed{
				{nodeKey: poolA.NodeKey(), reports: []detection.ExamReport{
					stabilityOnlyReport(40),
					stabilityOnlyReport(88),
				}},
			},
			wantKeys:   []string{poolA.NodeKey()},
			wantScores: map[string]int{poolA.NodeKey(): 88},
		},
		{
			name: "标签正确关联且池内元信息补齐",
			pool: []*subscription.Node{poolA, poolC},
			exams: []topNodesExamSeed{
				{nodeKey: poolA.NodeKey(), reports: []detection.ExamReport{stabilityOnlyReport(90)}},
				{nodeKey: poolC.NodeKey(), reports: []detection.ExamReport{stabilityOnlyReport(70)}},
			},
			tags: map[string][]string{
				poolA.NodeKey(): {"fast", "stable-high"},
			},
			wantKeys:   []string{poolA.NodeKey(), poolC.NodeKey()},
			wantScores: map[string]int{poolA.NodeKey(): 90, poolC.NodeKey(): 70},
			// poolC 无标签:tags 必须是 [] 而非 null。
			wantTagsEmptyArray: poolC.NodeKey(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, st := newTestServer(t, tc.pool)

			for _, seed := range tc.exams {
				for _, report := range seed.reports {
					if err := st.SaveExamHistory(seed.nodeKey, report); err != nil {
						t.Fatalf("save exam history: %v", err)
					}
				}
			}
			for nodeKey, tags := range tc.tags {
				if err := st.ReplaceNodeTags(nodeKey, tags); err != nil {
					t.Fatalf("replace node tags: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodGet, "/api/dashboard/top-nodes", nil)
			w := httptest.NewRecorder()
			srv.handleDashboardTopNodes(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if tc.wantRawBody != "" {
				if got := strings.TrimSpace(w.Body.String()); got != tc.wantRawBody {
					t.Fatalf("body = %q, want %q", got, tc.wantRawBody)
				}
				return
			}

			var items []topNodeResponseItem
			if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
				t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
			}
			if len(items) != len(tc.wantKeys) {
				t.Fatalf("len = %d, want %d (body=%s)", len(items), len(tc.wantKeys), w.Body.String())
			}
			byKey := make(map[string]topNodeResponseItem, len(items))
			for i, item := range items {
				if item.NodeKey != tc.wantKeys[i] {
					t.Fatalf("items[%d].node_key = %q, want %q (池顺序)", i, item.NodeKey, tc.wantKeys[i])
				}
				byKey[item.NodeKey] = item
			}
			for nodeKey, wantScore := range tc.wantScores {
				item := byKey[nodeKey]
				if item.Report.Stability == nil || item.Report.Stability.Score != wantScore {
					t.Fatalf("%s report score = %+v, want latest %d", nodeKey, item.Report.Stability, wantScore)
				}
			}
			// 池内元信息补齐校验(以 poolA/poolC 为例)。
			if item, ok := byKey[poolA.NodeKey()]; ok {
				if item.Region != "香港" || item.Source != "airport-a" || !item.Available || item.Type != "vmess" {
					t.Fatalf("pool meta mismatch: %+v", item)
				}
			}
			if item, ok := byKey[poolC.NodeKey()]; ok {
				if item.Region != "美国" || item.Source != "airport-a" || !item.Available {
					t.Fatalf("pool meta mismatch: %+v", item)
				}
			}
			// 标签关联校验。
			for nodeKey, wantTags := range tc.tags {
				item := byKey[nodeKey]
				if len(item.Tags) != len(wantTags) {
					t.Fatalf("%s tags = %v, want %v", nodeKey, item.Tags, wantTags)
				}
				for i, tag := range wantTags {
					if item.Tags[i] != tag {
						t.Fatalf("%s tags = %v, want %v", nodeKey, item.Tags, wantTags)
					}
				}
			}
			if tc.wantTagsEmptyArray != "" {
				item := byKey[tc.wantTagsEmptyArray]
				if item.Tags == nil {
					t.Fatalf("%s tags must be empty array, got null", tc.wantTagsEmptyArray)
				}
				if !strings.Contains(w.Body.String(), `"tags":[]`) {
					t.Fatalf("body missing \"tags\":[] for untagged node: %s", w.Body.String())
				}
			}
		})
	}
}
