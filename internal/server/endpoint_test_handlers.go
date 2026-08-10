package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// endpointDeliverableNodes 计算该订阅地址"此刻会下发的节点集合":
// 池 → 精选候选集替换 → 全局过滤链(宕机免疫:监控集合节点跳过可用性/延迟,
// issue #101)→ 端点条件 → 名称标准化(跳过槽位节点)
// → 名称槽位(ADR 0047 / issue #96)→ 精选项别名(命名链最终层,issue #85),
// 与 /sub 生成链同源(所见即所得)。
// 拉取验证、池快照、现场实测、后台预览四处共用这一个选择逻辑(ADR 0028 决策 1)。
// 池与自建节点按端点属主分片(ticket 07;UserID 0 = 全局池)。
func (s *Server) endpointDeliverableNodes(ep *store.Endpoint) []*subscription.Node {
	picks := s.endpointNodePicks(ep)
	nodes := s.filteredNodesForDelivery(s.nodes.NodesForUser(ep.UserID), ep.UserID, picks, s.monitorImmuneKeys(ep.UserID))
	nodes = s.applyConditions(nodes, ep)
	nodes = s.standardizeNodesForEndpoint(nodes, ep, ep.UserID)
	nodes = s.applyNameSlots(nodes, ep.UserID)
	return applyNodePickAliases(nodes, picks)
}

// formatCheckResult 单格式拉取验证结果。
type formatCheckResult struct {
	Valid      bool   `json:"valid"`
	NodeCount  int    `json:"node_count"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// poolSnapshotView 会下发节点的池状态快照。口径与机场测试评分一致:
// 可用数/均值只算可用节点,地区覆盖 = 非空地区去重计数。
type poolSnapshotView struct {
	Total       int      `json:"total"`
	Available   int      `json:"available"`
	MeanLatency float64  `json:"mean_latency_ms"`
	RegionCount int      `json:"region_count"`
	Regions     []string `json:"regions"`
}

// endpointTestResponse 拉取验证(双格式)+ 池快照。
type endpointTestResponse struct {
	Pull     map[string]formatCheckResult `json:"pull"`
	Snapshot poolSnapshotView             `json:"snapshot"`
}

// handleEndpointTest 订阅测试(拉取验证 + 池快照):走内部生成链,不发真实 HTTP、
// 不记 pull_logs(ADR 0028 决策 1/2)。禁用态可测(决策 4),端点存在即可。
// ticket 07: 校验属主,行属他人 404。
func (s *Server) handleEndpointTest(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.GetEndpointByIDForUser(EffectiveUserID(scope), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	nodes := s.endpointDeliverableNodes(ep)
	writeJSON(w, endpointTestResponse{
		Pull: map[string]formatCheckResult{
			"clash": s.validateFormat(nodes, "clash", ep),
			"v2ray": s.validateFormat(nodes, "v2ray", ep),
		},
		Snapshot: snapshotDeliverable(nodes),
	})
}

// validateFormat 生成单格式订阅内容并校验合法性。空集合不生成(生成器对空集报错),
// 直接判 invalid 并给出原因。按端点解析模板(四级回退,含端点绑定),与真实 /sub 同口径。
func (s *Server) validateFormat(nodes []*subscription.Node, format string, ep *store.Endpoint) formatCheckResult {
	if len(nodes) == 0 {
		return formatCheckResult{Valid: false, Error: "no deliverable nodes"}
	}
	start := time.Now()
	data, _, err := s.renderSubscriptionForEndpoint(nodes, format, ep)
	result := formatCheckResult{DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		result.Error = err.Error()
		return result
	}

	var count int
	var vErr error
	if format == "v2ray" {
		count, vErr = validateV2RayContent(data)
	} else {
		count, vErr = validateClashContent(data)
	}
	if vErr != nil {
		result.Error = vErr.Error()
		return result
	}
	result.Valid = true
	result.NodeCount = count
	return result
}

// validateClashContent 校验 Clash 产出:YAML 可解析,返回 proxies 条目数。
func validateClashContent(data []byte) (int, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("clash output not parseable: %w", err)
	}
	proxies, _ := doc["proxies"].([]any)
	return len(proxies), nil
}

// validateV2RayContent 校验 v2ray 产出:base64 可解码,返回非空分享链接行数。
func validateV2RayContent(data []byte) (int, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("v2ray output not decodable: %w", err)
	}
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("v2ray output has no share links")
	}
	return count, nil
}

// snapshotDeliverable 计算池快照。返回新对象,不改动入参。
func snapshotDeliverable(nodes []*subscription.Node) poolSnapshotView {
	snap := poolSnapshotView{Total: len(nodes), Regions: []string{}}
	regionSet := make(map[string]bool)
	latencySum := 0
	for _, n := range nodes {
		if n.Available {
			snap.Available++
			latencySum += n.Latency
		}
		if n.Region != "" {
			regionSet[n.Region] = true
		}
	}
	if snap.Available > 0 {
		snap.MeanLatency = float64(latencySum) / float64(snap.Available)
	}
	for region := range regionSet {
		snap.Regions = append(snap.Regions, region)
	}
	sort.Strings(snap.Regions)
	snap.RegionCount = len(snap.Regions)
	return snap
}

// handleEndpointTestProbe 现场实测(ADR 0028 决策 3/5):立即返回 run 句柄,
// 后台 goroutine 用 ProbeCore 对会下发节点抽样(full=true 全量)检活并写回池;
// run 状态只存内存,重启即弃。禁用态可测(决策 4)。
// ticket 07: 校验属主,行属他人 404。
func (s *Server) handleEndpointTestProbe(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.GetEndpointByIDForUser(EffectiveUserID(scope), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Full bool `json:"full"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
	}

	nodes := s.endpointDeliverableNodes(ep)
	if len(nodes) == 0 {
		http.Error(w, "no deliverable nodes to probe", http.StatusBadRequest)
		return
	}

	run := s.probeRuns.create(ep.ID, body.Full, len(nodes))
	go s.runEndpointProbe(run.RunID, nodes, body.Full)
	writeJSON(w, run)
}

// runEndpointProbe 后台执行:抽样检活 + 写回池(与健康检查同语义),
// 进度经 hooks 落入内存注册表。
func (s *Server) runEndpointProbe(runID string, nodes []*subscription.Node, full bool) {
	core := airporttest.NewProbeCore(s.probeChecker, s.nodes)
	_, err := core.Probe(context.Background(), nodes, full, airporttest.ProbeHooks{
		OnSampled: func(sampled int) error {
			s.probeRuns.markSampled(runID, sampled)
			return nil
		},
		OnProgress: func(checked int) {
			s.probeRuns.markChecked(runID, checked)
		},
	})
	if err != nil {
		s.logger.Warn("endpoint probe failed", "run", runID, "error", err)
	}
	s.probeRuns.finish(runID, err)
}

// handleGetEndpointTestProbe 轮询实测进度。run 只存内存且按端点隔离:
// 不存在(含重启丢失、TTL 过期、属于其他端点)一律 404,前端据此提示重跑。
// ticket 07: 校验属主,行属他人 404。
func (s *Server) handleGetEndpointTestProbe(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// 校验属主(ticket 07):行属他人同样 404,不暴露存在性。
	if _, err := s.st.GetEndpointByIDForUser(EffectiveUserID(scope), id); err != nil {
		http.NotFound(w, r)
		return
	}
	run, ok := s.probeRuns.get(r.PathValue("runId"))
	if !ok || run.EndpointID != id {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, run)
}

// endpointAvailability 列表项的可用性汇总(池在内存,实时算)。
type endpointAvailability struct {
	Available int `json:"available"`
	Total     int `json:"total"`
}

// endpointListItem 列表响应项:既有 Endpoint 字段原样内嵌,加性附加可用性。
type endpointListItem struct {
	*store.Endpoint
	Availability endpointAvailability `json:"availability"`
}

// availabilityFor 计算单个端点会下发集合的可用 x/y。
// nodes 必须是与下发同口径的已收窄集合(调用方负责:无精选传全局过滤链 base,
// 有精选传 filteredNodesWithPicks 的产物,精选先于 filt.Apply 收窄);
// 此处只再套用端点条件;名称标准化不影响计数,跳过。
func (s *Server) availabilityFor(nodes []*subscription.Node, ep *store.Endpoint) endpointAvailability {
	nodes = s.applyConditions(nodes, ep)
	result := endpointAvailability{Total: len(nodes)}
	for _, n := range nodes {
		if n.Available {
			result.Available++
		}
	}
	return result
}
