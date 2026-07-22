package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/filter"
	"github.com/taliove/proxyhub/internal/generator"
	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/healthcheck"
	"github.com/taliove/proxyhub/internal/poolops"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// NodeSource 节点来源接口（由 Aggregator 实现）
type NodeSource interface {
	Nodes() []*subscription.Node
	LastUpdate() time.Time
	// StartRefreshJob 经 jobs 运行时发起全量刷新任务(trigger 记入 params)。
	// 返回 jobs 行 id/任务 key/是否新启动;同 key 重复触发附加到进行中任务,
	// 与进行中的单机场刷新冲突时返回 aggregator.ErrRefreshConflict。
	StartRefreshJob(trigger string) (jobID int64, key string, started bool, err error)
	// StartAirportRefreshJob 发起单机场刷新(只拉取入池,不含健康检查)。
	// 与全量或同机场的进行中刷新冲突时返回 aggregator.ErrRefreshConflict。
	StartAirportRefreshJob(trigger string, airportID int64) (jobID int64, key string, started bool, err error)
	// CancelRefresh 取消指定 key 的刷新任务;无进行中任务返回 false。
	CancelRefresh(key string) bool
	// UpdateNodeTestResult 将单节点即时测试结果写回内存池（按 NodeKey 匹配）。
	// failReason/failDetail 为失败原因分类与短详情(成功时传空串清空,见 ticket 0017)。
	// 找到返回 true；池中无此节点返回 false。
	UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool
	// UpdateNodeIdentity 按 NodeKey 更新内存池中节点的身份字段(名称/地区)。
	// name/region 为空表示本次不改该字段。找到返回 true；池中无此节点返回 false。
	UpdateNodeIdentity(nodeKey, name, region string) bool
	// PurgeAirportNodes 一键清空机场节点(内存池+DB 双清,自建豁免,屏蔽/覆盖保留)。
	// 有刷新任务进行中时返回 aggregator.ErrPurgeConflict(拒绝而非等待)。
	PurgeAirportNodes() (int, error)
}

// Server HTTP 服务
type Server struct {
	cfg              *config.Config
	st               *store.Store
	nodes            NodeSource
	webFS            embed.FS
	sessions         *SessionManager
	logger           *slog.Logger
	detectionService *DetectionService
	detectionJobs    *DetectionServiceJobs
	geo              *geoip.Resolver
	examJobs         *detection.ExamJobManager
	batchExamJobs    *detection.BatchExamJobManager
	testOrchestrator *airporttest.Orchestrator
	// self-node region resolution seams: real by default, overridable in tests
	// to drive the suggest/save paths without touching DNS or the embedded DB.
	lookupHost    func(host string) ([]string, error)
	countryLookup func(ip string) (string, error)
}

// New 创建 HTTP 服务
func New(cfg *config.Config, st *store.Store, nodes NodeSource, webFS embed.FS, logger *slog.Logger, detectionService *DetectionService, geo *geoip.Resolver) *Server {
	s := &Server{
		cfg:              cfg,
		st:               st,
		nodes:            nodes,
		webFS:            webFS,
		sessions:         NewSessionManager(12 * time.Hour),
		logger:           logger,
		detectionService: detectionService,
		geo:              geo,
		lookupHost:       net.LookupHost,
		countryLookup:    geoip.LookupCountry,
	}

	// 体检任务管理器:runner 复用 detectionService.ExamStream(逻辑零改动),
	// 自然完成回调把落历史从 handler 搬进任务生命周期(与连接无关)。
	// 生命周期落 jobs 表(重启标记 interrupted);持久化错误记日志,不静默吞。
	if detectionService != nil {
		s.examJobs = detection.NewExamJobManager(
			detectionService.ExamStream,
			s.onExamComplete,
			detection.WithExamJobStore(st.Jobs()),
			detection.WithExamErrorHandler(func(err error) {
				logger.Warn("exam job persistence", "error", err)
			}),
		)

		// 批量体检任务管理器:精简体检(出网 + 稳定性 + 基准下行),游标续跑,
		// 每节点完成回调复用 onExamComplete(落历史 + 触发标签重算)。
		s.batchExamJobs = detection.NewBatchExamJobManager(
			detectionService.ExamStreamSimplified,
			s.onExamComplete,
			detection.WithBatchExamJobStore(st.Jobs()),
			detection.WithBatchExamErrorHandler(func(err error) {
				logger.Warn("batch exam job persistence", "error", err)
			}),
		)
	}

	// 初始化机场测试编排器
	storeAdapter := airporttest.NewStoreAdapter(st)
	healthChecker := NewHealthCheckAdapter(healthcheck.NewChecker(
		cfg.HealthCheck.Timeout.Latency,
		cfg.HealthCheck.Timeout.Request,
		cfg.HealthCheck.TestURL,
		cfg.HealthCheck.Concurrent,
	))
	poolOps := poolops.NewStoreAdapter(st)
	s.testOrchestrator = airporttest.NewOrchestratorWithPoolOps(storeAdapter, healthChecker, nodes, poolOps)

	return s
}

// onExamComplete 体检自然完成的收口:落历史 + 按该节点重算自动标签 + 空/Unknown地区回写。
// 三步均为 best-effort:任一失败只记日志,不影响体检结果本身(下一场体检会再算)。
func (s *Server) onExamComplete(nodeKey string, report detection.ExamReport) {
	if err := s.st.SaveExamHistory(nodeKey, report); err != nil {
		s.logger.Warn("save exam history failed", "error", err)
	}
	if err := s.st.RecomputeNodeTags(nodeKey); err != nil {
		s.logger.Warn("recompute node tags after exam failed", "error", err)
	}
	s.writebackRegionIfNeeded(nodeKey, report)
}

// writebackRegionIfNeeded writes back node region from exam egress data.
// Egress country code is the real exit point (ground truth), while GeoIP is just a guess.
// Therefore, when egress data is available, ALWAYS overwrite the node's region with it,
// even if the node already has a non-empty region value (which may be an incorrect GeoIP guess).
// Nodes are updated in memory pool (airport nodes) or database (self-hosted nodes).
// No egress data means no writeback. Best-effort: failures are logged but don't block exam completion.
func (s *Server) writebackRegionIfNeeded(nodeKey string, report detection.ExamReport) {
	// No egress info or no country code - cannot writeback
	if report.Egress == nil || report.Egress.IPv4 == nil || report.Egress.IPv4.CountryCode == "" {
		return
	}

	countryCode := report.Egress.IPv4.CountryCode

	// Try updating airport node (memory pool)
	if s.updateAirportNodeRegion(nodeKey, countryCode) {
		return
	}

	// Try updating self-hosted node (database)
	if err := s.updateSelfHostedNodeRegion(nodeKey, countryCode); err != nil {
		// 404 means node not in self-hosted table (may have been deleted or is airport node with refreshed pool), silently ignore
		if err != store.ErrNotFound {
			s.logger.Warn("writeback self-hosted node region failed", "nodeKey", nodeKey, "error", err)
		}
	}
}

// updateAirportNodeRegion attempts to update an airport node's region (memory pool, by traversing NodeSource).
// ALWAYS overwrites region with egress country code (egress is ground truth, existing region may be wrong GeoIP guess).
// Returns true if node found and updated, false if not found.
// Airport node region lives in memory pool; persistence depends on storage model (currently nodes table doesn't store region, rebuilt on refresh).
func (s *Server) updateAirportNodeRegion(nodeKey, countryCode string) bool {
	if s.nodes == nil {
		return false
	}
	for _, n := range s.nodes.Nodes() {
		if n.NodeKey() != nodeKey {
			continue
		}
		// Skip self-hosted nodes (handled by updateSelfHostedNodeRegion)
		if n.Source == subscription.SourceSelfHosted {
			return false
		}
		// Always overwrite region with egress country code
		n.Region = countryCode
		return true
	}
	return false
}

// updateSelfHostedNodeRegion attempts to update a self-hosted node's region_code (database self_hosted_nodes table).
// Located by server:port (NodeKey without SNI is sufficient for self-hosted nodes).
// ALWAYS overwrites region with egress country code (egress is ground truth, existing region may be wrong GeoIP guess).
// Returns store.ErrNotFound if not found.
func (s *Server) updateSelfHostedNodeRegion(nodeKey, countryCode string) error {
	nodes, err := s.st.ListAllSelfHostedNodes()
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.ToNode().NodeKey() != nodeKey {
			continue
		}
		// Always overwrite region with egress country code
		n.RegionCode = countryCode
		if err := s.st.UpdateSelfHostedNode(n); err != nil {
			return err
		}
		// Sync memory pool region so /nodes reflects egress truth immediately
		// (ticket 47); name unchanged here, pass empty to leave it.
		s.syncSelfHostedNodeIdentity(nodeKey, "", countryCode)
		return nil
	}
	return store.ErrNotFound
}

// syncSelfHostedNodeIdentity pushes a self-hosted node's new name/region into the
// memory pool by NodeKey, so the admin /nodes list reflects renames and region
// writebacks immediately instead of waiting for the next aggregation refresh
// (ticket 47). name/region empty means "leave that field". No-op if the pool has
// no such node (self-hosted node not yet merged into the pool).
func (s *Server) syncSelfHostedNodeIdentity(nodeKey, name, regionCode string) {
	if s.nodes == nil {
		return
	}
	s.nodes.UpdateNodeIdentity(nodeKey, name, regionCode)
}

// RecoverJobs 重启恢复:把上次进程遗留的 running 任务恢复或标记中断。
// 单发任务(exam)标记 interrupted,批量任务(batch_exam)从游标续跑。
// best-effort:失败只记日志。由 main 在对外服务前调用。
func (s *Server) RecoverJobs() {
	if s.examJobs != nil {
		if err := s.examJobs.RecoverInterrupted(); err != nil {
			s.logger.Warn("recover interrupted exam jobs failed", "error", err)
		}
	}
	if s.batchExamJobs != nil {
		if err := s.batchExamJobs.RecoverInterrupted(); err != nil {
			s.logger.Warn("recover interrupted batch exam jobs failed", "error", err)
		}
	}
	if s.detectionJobs != nil {
		if err := s.detectionJobs.Recover(); err != nil {
			s.logger.Warn("recover batch detection job failed", "error", err)
		}
	}
}

// SetDetectionJobs 注入基于 jobs 运行时的批量检测服务(main 接线;
// 批量触发/取消/状态与通用任务 API 走它,旧的 goroutine+轮询机制已退役)。
func (s *Server) SetDetectionJobs(dj *DetectionServiceJobs) {
	s.detectionJobs = dj
}

// examSweepInterval 体检任务 TTL 清扫周期。
const examSweepInterval = time.Minute

// StartExamSweeper 周期清扫超过 TTL 的已完成体检任务,随 ctx 结束退出。
// 由 main 在应用根 context 下后台启动;测试不启用,避免 goroutine 泄漏与时钟竞态。
func (s *Server) StartExamSweeper(ctx context.Context) {
	if s.examJobs == nil {
		return
	}
	ticker := time.NewTicker(examSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.examJobs.SweepExpired()
		}
	}
}

// Handler 构建路由
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 系统初始化（无需认证，仅未初始化时可用）
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("GET /api/status", s.handleStatus)

	// 认证
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))

	// 订阅地址管理
	mux.HandleFunc("GET /api/endpoints", s.requireAuth(s.handleListEndpoints))
	mux.HandleFunc("POST /api/endpoints", s.requireAuth(s.handleCreateEndpoint))
	mux.HandleFunc("POST /api/endpoints/{id}/toggle", s.requireAuth(s.handleToggleEndpoint))
	mux.HandleFunc("PUT /api/endpoints/{id}/name-config", s.requireAuth(s.handleUpdateEndpointNameConfig))
	mux.HandleFunc("PUT /api/endpoints/{id}/conditions", s.requireAuth(s.handleUpdateEndpointConditions))
	mux.HandleFunc("POST /api/endpoints/preview-conditions", s.requireAuth(s.handlePreviewConditions))
	mux.HandleFunc("DELETE /api/endpoints/{id}", s.requireAuth(s.handleDeleteEndpoint))
	mux.HandleFunc("GET /api/endpoints/{id}/stats", s.requireAuth(s.handleEndpointStats))
	mux.HandleFunc("GET /api/endpoints/{id}/preview", s.requireAuth(s.handleEndpointPreview))

	// 机场管理
	mux.HandleFunc("GET /api/airports", s.requireAuth(s.handleListAirports))
	mux.HandleFunc("GET /api/airports/abbr-suggest", s.requireAuth(s.handleSuggestAbbr))
	mux.HandleFunc("POST /api/airports", s.requireAuth(s.handleCreateAirport))
	mux.HandleFunc("PUT /api/airports/{id}", s.requireAuth(s.handleUpdateAirport))
	mux.HandleFunc("POST /api/airports/{id}/toggle", s.requireAuth(s.handleToggleAirport))
	mux.HandleFunc("POST /api/airports/{id}/refresh", s.requireAuth(s.handleAirportRefresh))
	mux.HandleFunc("DELETE /api/airports/{id}", s.requireAuth(s.handleDeleteAirport))

	// 机场测试（诊断+抽样检活+评分）
	mux.HandleFunc("POST /api/airports/{id}/test", s.requireAuth(s.handleAirportTest))
	mux.HandleFunc("GET /api/airports/{id}/test/runs/{runId}", s.requireAuth(s.handleGetAirportTestRun))
	mux.HandleFunc("GET /api/airports/{id}/test/runs", s.requireAuth(s.handleListAirportTestRuns))

	// 节点状态
	mux.HandleFunc("GET /api/nodes", s.requireAuth(s.handleListNodes))
	// 节点分享 URI 生成（用于生成二维码）
	mux.HandleFunc("GET /api/nodes/{nodeKey}/share-uri", s.requireAuth(s.handleNodeShareURI))

	// 机场节点屏蔽（按 NodeKey 精确拉黑，跨刷新持久）
	mux.HandleFunc("POST /api/nodes/block", s.requireAuth(s.handleBlockNode))
	mux.HandleFunc("POST /api/nodes/unblock", s.requireAuth(s.handleUnblockNode))

	// 批量屏蔽：按机场（source）或显式 node_keys 一次性拉黑/取消
	mux.HandleFunc("POST /api/nodes/batch-block", s.requireAuth(s.handleBatchBlockNodes))
	mux.HandleFunc("POST /api/nodes/batch-unblock", s.requireAuth(s.handleBatchUnblockNodes))

	// 批量刷新名称：重跑地区识别+标准化
	mux.HandleFunc("POST /api/nodes/refresh-names", s.requireAuth(s.handleRefreshNames))

	// 自建节点管理（增/改/删/启停）
	mux.HandleFunc("GET /api/self-nodes", s.requireAuth(s.handleListSelfNodes))
	mux.HandleFunc("GET /api/self-nodes/suggest", s.requireAuth(s.handleSuggestSelfNode))
	mux.HandleFunc("POST /api/self-nodes", s.requireAuth(s.handleCreateSelfNode))
	mux.HandleFunc("PUT /api/self-nodes/{id}", s.requireAuth(s.handleUpdateSelfNode))
	mux.HandleFunc("DELETE /api/self-nodes/{id}", s.requireAuth(s.handleDeleteSelfNode))
	mux.HandleFunc("POST /api/self-nodes/{id}/toggle", s.requireAuth(s.handleToggleSelfNode))

	// 聚合器手动刷新
	mux.HandleFunc("POST /api/aggregator/refresh", s.requireAuth(s.handleManualRefresh))

	// 刷新历史
	mux.HandleFunc("GET /api/refresh/runs", s.requireAuth(s.handleListRefreshRuns))
	mux.HandleFunc("GET /api/refresh/runs/{id}", s.requireAuth(s.handleGetRefreshRun))

	// 节点解锁检测
	mux.HandleFunc("POST /api/detection/trigger", s.requireAuth(s.handleTriggerDetection))
	mux.HandleFunc("POST /api/detection/cancel", s.requireAuth(s.handleCancelDetection))
	mux.HandleFunc("GET /api/detection/status", s.requireAuth(s.handleDetectionStatus))

	// 通用任务 API(任务中心)
	mux.HandleFunc("GET /api/jobs", s.requireAuth(s.handleListJobs))
	mux.HandleFunc("GET /api/jobs/{id}", s.requireAuth(s.handleGetJobDetail))
	mux.HandleFunc("POST /api/jobs/{kind}/{key}/cancel", s.requireAuth(s.handleCancelJob))
	mux.HandleFunc("POST /api/nodes/test", s.requireAuth(s.handleTestNode))
	mux.HandleFunc("GET /api/nodes/test/stream", s.requireAuth(s.handleTestNodeStream))
	mux.HandleFunc("GET /api/nodes/exam/stream", s.requireAuth(s.handleNodeExamStream))
	mux.HandleFunc("POST /api/nodes/exam/cancel", s.requireAuth(s.handleNodeExamCancel))
	mux.HandleFunc("GET /api/nodes/exam/latest", s.requireAuth(s.handleGetExamLatest))
	mux.HandleFunc("GET /api/nodes/exam/history", s.requireAuth(s.handleGetExamHistory))
	mux.HandleFunc("POST /api/nodes/exam/batch", s.requireAuth(s.handleBatchExam))
	mux.HandleFunc("GET /api/nodes/exam/batch/stream", s.requireAuth(s.handleBatchExamStream))
	mux.HandleFunc("POST /api/nodes/exam/batch/cancel", s.requireAuth(s.handleBatchExamCancel))

	// 节点管理（覆盖层 + 清理）
	mux.HandleFunc("PUT /api/nodes/override", s.requireAuth(s.handleSetNodeOverride))
	mux.HandleFunc("DELETE /api/nodes/override", s.requireAuth(s.handleClearNodeOverride))
	mux.HandleFunc("POST /api/nodes/cleanup", s.requireAuth(s.handleCleanupNodes))
	mux.HandleFunc("POST /api/nodes/purge-airport", s.requireAuth(s.handlePurgeAirportNodes))

	// 系统设置
	mux.HandleFunc("GET /api/settings", s.requireAuth(s.handleGetSettings))
	mux.HandleFunc("POST /api/settings", s.requireAuth(s.handleSaveSettings))

	// 检测目标配置
	mux.HandleFunc("GET /api/settings/detection-targets", s.requireAuth(s.handleGetDetectionTargets))
	mux.HandleFunc("PUT /api/settings/detection-targets", s.requireAuth(s.handleSaveDetectionTargets))

	// 晚间标签重算调度配置(schedule_retag_time / schedule_retag_enabled)
	mux.HandleFunc("GET /api/settings/schedule", s.requireAuth(s.handleGetSchedule))
	mux.HandleFunc("PUT /api/settings/schedule", s.requireAuth(s.handleSaveSchedule))

	// 地区白名单
	mux.HandleFunc("GET /api/settings/region-whitelist", s.requireAuth(s.handleGetRegionWhitelist))
	mux.HandleFunc("POST /api/settings/region-whitelist", s.requireAuth(s.handleSetRegionWhitelist))
	mux.HandleFunc("GET /api/settings/regions", s.requireAuth(s.handleListRegions))

	// 配置模板（Clash 订阅骨架，含 hosts/dns/proxy-groups/rules）
	mux.HandleFunc("GET /api/settings/template", s.requireAuth(s.handleGetTemplate))
	mux.HandleFunc("PUT /api/settings/template", s.requireAuth(s.handleSaveTemplate))
	mux.HandleFunc("POST /api/settings/template/reset", s.requireAuth(s.handleResetTemplate))

	// 仪表盘统计 + 优质节点聚合
	mux.HandleFunc("GET /api/dashboard/stats", s.requireAuth(s.handleDashboardStats))
	mux.HandleFunc("GET /api/dashboard/top-nodes", s.requireAuth(s.handleDashboardTopNodes))

	// 安全审计（登录/封禁事件流水 + 当前封禁 IP 管理）
	mux.HandleFunc("GET /api/audit/events", s.requireAuth(s.handleAuditEvents))
	mux.HandleFunc("GET /api/audit/banned", s.requireAuth(s.handleBannedIPs))
	mux.HandleFunc("POST /api/audit/unban", s.requireAuth(s.handleUnbanIP))

	// 访问统计（全局汇总 + 拉取趋势）
	mux.HandleFunc("GET /api/stats/global", s.requireAuth(s.handleGlobalStats))
	mux.HandleFunc("GET /api/stats/trend", s.requireAuth(s.handlePullTrend))

	// 订阅拉取端点（随机 Path + Token，公开访问）
	mux.HandleFunc("GET /sub/{path}", s.handleSubscription)

	// 健康检查端点（供反向代理探活）
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Vue SPA（其余路径交给前端路由，找不到的静态资源回退到 index.html）
	mux.HandleFunc("GET /", s.handleSPA)

	// Site Path 边界：配置后仅 /<site-path>/ 下可达，其余一律 404；未配置则透传
	return s.sitePathMiddleware(mux)
}

// handleStatus 返回系统是否已初始化（前端据此决定进入向导还是登录）
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.st.IsSystemInitialized()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"initialized": initialized})
}

// handleSPA 提供内嵌的 Vue 单页应用，支持前端路由的 history 模式回退
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	webRoot, err := fs.Sub(s.webFS, "web")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusInternalServerError)
		return
	}

	reqPath := strings.TrimPrefix(r.URL.Path, "/")
	if reqPath == "" {
		reqPath = "index.html"
	}

	// 命中真实静态文件则直接返回，否则回退到 index.html（前端路由接管）
	if f, err := webRoot.Open(reqPath); err == nil {
		f.Close()
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
		return
	}

	// 回退到 index.html
	data, _ := fs.ReadFile(webRoot, "index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// clientIP 提取客户端 IP（优先 X-Forwarded-For，反向代理场景）
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleSubscription 订阅拉取端点：/sub/{path}?token=xxx&format=clash|v2ray
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")

	ep, err := s.st.GetEndpointByPath(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Token 校验（常数时间比较，防时序攻击）
	token := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(ep.Token)) != 1 {
		http.NotFound(w, r) // 故意返回 404 而不是 403，不暴露端点存在
		return
	}

	if !ep.Enabled {
		http.NotFound(w, r)
		return
	}

	nodes := s.nodes.Nodes()
	if len(nodes) == 0 {
		http.Error(w, "no available nodes yet, try again later", http.StatusServiceUnavailable)
		return
	}

	// 订阅时过滤链：白名单 → 黑名单 → 机场屏蔽，自建节点全程豁免（见 ADR 0005/0009）
	nodes = s.filteredNodes(nodes)
	// 该订阅地址的节点范围条件(动态查询,见 internal/subfilter);空条件=全量(零回归)
	nodes = s.applyConditions(nodes, ep)
	if len(nodes) == 0 {
		// 过滤链过窄把节点池清空：与池未就绪同样返回 503，避免生成空订阅落到 500，
		// 也与后台预览（返回空清单）保持一致
		http.Error(w, "no available nodes yet, try again later", http.StatusServiceUnavailable)
		return
	}

	// 节点名称标准化（见 ADR 0012）：按该订阅地址的生效配置（端点覆盖 → 全局回退）统一名称。
	nodes = s.standardizeNodesForEndpoint(nodes, ep)

	// 格式：显式参数优先，否则按 User-Agent 猜测，默认 Clash
	format := r.URL.Query().Get("format")
	if format == "" {
		ua := strings.ToLower(r.Header.Get("User-Agent"))
		if strings.Contains(ua, "v2ray") || strings.Contains(ua, "shadowrocket") {
			format = "v2ray"
		} else {
			format = "clash"
		}
	}

	data, contentType, err := s.renderSubscription(nodes, format)
	if err != nil {
		s.logger.Error("generate subscription failed", "format", format, "error", err)
		http.Error(w, "generate subscription failed", http.StatusInternalServerError)
		return
	}

	// 记录拉取统计
	if err := s.st.RecordPull(store.PullRecord{
		EndpointID: ep.ID,
		IP:         clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
	}); err != nil {
		s.logger.Warn("record pull failed", "error", err)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Profile-Update-Interval", "1") // 建议客户端每小时更新
	w.Write(data)
}

// handleLogin 管理后台登录（受 IP2Ban 保护）
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	// 127.0.0.1 永不封禁（本地开发）
	if ip != "127.0.0.1" {
		banned, err := s.st.IsBanned(ip, time.Now())
		if err != nil {
			s.logger.Error("check ban failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if banned {
			http.Error(w, "too many failed attempts, try later", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	policy := s.loadSecurityPolicy()

	// 蜜罐检测：任何针对高敏感用户名（admin/root 等）的尝试，立即封禁 IP。
	// 合法账号在初始化时已禁止使用这些名字，因此命中必为攻击。
	// 127.0.0.1 豁免（本地开发）
	if ip != "127.0.0.1" && isHoneypotUsername(req.Username) {
		bannedUntil, err := s.st.BanIP(ip, policy.BanDuration, time.Now())
		if err != nil {
			s.logger.Error("honeypot ban failed", "error", err)
		}
		s.logger.Warn("honeypot username hit, ip banned", "ip", ip, "username", req.Username)
		s.recordAudit("honeypot_ban", ip, req.Username,
			fmt.Sprintf("蜜罐命中，封禁至 %s", bannedUntil.Format("2006-01-02 15:04:05")))
		http.Error(w, "too many failed attempts, try later", http.StatusForbidden)
		return
	}

	if !s.verifyCredentials(req.Username, req.Password) {
		now := time.Now()
		nowBanned, err := s.st.RecordLoginFailure(ip,
			policy.BanThreshold, policy.BanDuration, now)
		if err != nil {
			s.logger.Error("record login failure failed", "error", err)
		}
		if nowBanned {
			s.logger.Warn("ip banned after repeated failures", "ip", ip)
			bannedUntil := now.Add(policy.BanDuration)
			s.recordAudit("threshold_ban", ip, req.Username,
				fmt.Sprintf("连续失败达阈值 %d，封禁至 %s",
					policy.BanThreshold, bannedUntil.Format("2006-01-02 15:04:05")))
		} else {
			s.recordAudit("login_failure", ip, req.Username, "")
		}
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// 登录成功：清空失败计数，发放会话
	s.st.ResetLoginFailures(ip)
	s.recordAudit("login_success", ip, req.Username, "")
	token, err := s.sessions.Create()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((12 * time.Hour).Seconds()),
	})
	writeJSON(w, map[string]bool{"ok": true})
}

// verifyCredentials 校验用户名和密码（bcrypt）
func (s *Server) verifyCredentials(username, password string) bool {
	storedUser, err := s.st.GetSetting("admin_user")
	if err != nil {
		return false
	}
	storedHash, err := s.st.GetSetting("admin_pass_hash")
	if err != nil {
		return false
	}

	userMatch := subtle.ConstantTimeCompare([]byte(username), []byte(storedUser)) == 1
	passMatch := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) == nil
	return userMatch && passMatch
}

// requireAuth 会话鉴权中间件
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || !s.sessions.Validate(cookie.Value) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.sessions.Destroy(cookie.Value)
	}
	// 让浏览器立即清除会话 cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, map[string]bool{"ok": true})
}

// handleMe 返回当前登录用户信息，供前端刷新后恢复登录态
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	username, err := s.st.GetSetting("admin_user")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"username": username})
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	eps, err := s.st.ListEndpoints()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, eps)
}

func (s *Server) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Alias) == "" {
		http.Error(w, "alias is required", http.StatusBadRequest)
		return
	}

	ep, err := s.st.CreateEndpoint(strings.TrimSpace(req.Alias))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ep)
}

func (s *Server) handleToggleEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ep, err := s.st.GetEndpointByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := s.st.SetEndpointEnabled(id, !ep.Enabled); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"enabled": !ep.Enabled})
}

// handleUpdateEndpointNameConfig 设置订阅地址的节点名称标准化覆盖（见 ADR 0012）。
func (s *Server) handleUpdateEndpointNameConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		NameMode     string `json:"name_mode"`
		NameTemplate string `json:"name_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := s.st.UpdateEndpointNameConfig(id, req.NameMode, req.NameTemplate); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// 非法 name_mode 等参数错误回 400,其余 500
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.st.DeleteEndpoint(id); err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleEndpointStats(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	stats, err := s.st.EndpointStats(id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if stats == nil {
		stats = []*store.IPStat{}
	}
	writeJSON(w, stats)
}

// unlockResultView 单个目标的解锁检测结果(对外视图)
type unlockResultView struct {
	Available bool    `json:"available"`
	Latency   int     `json:"latency"`
	Error     string  `json:"error,omitempty"`
	DownMbps  float64 `json:"down_mbps,omitempty"`
	UpMbps    float64 `json:"up_mbps,omitempty"`
	// Level/Region 仅专用解锁判定填充;generic/error 结果留空,序列化省略。
	Level  string `json:"level,omitempty"`  // 解锁级别:full/originals_only/blocked
	Region string `json:"region,omitempty"` // 命中区域国家码(如 US/HK)
}

// nodeView 是节点对外暴露的只读视图（隐藏协议密钥等敏感字段）
type nodeView struct {
	Name        string `json:"name"`         // 机场原名
	DisplayName string `json:"display_name"` // 标准化名（未标准化时为空）
	Type        string `json:"type"`
	Server      string `json:"server"` // 服务器地址（非敏感，展示用）
	Port        int    `json:"port"`
	Network     string `json:"network,omitempty"` // 传输方式 tcp/ws/grpc
	TLS         bool   `json:"tls"`
	SNI         string `json:"sni,omitempty"`
	// 排障用协议参数(ticket 0016):数据早已落库,此前被视图裁剪。
	// uuid/password 属凭证,仍不透出。
	Cipher          string `json:"cipher,omitempty"`
	AlterID         int    `json:"alter_id,omitempty"`
	Plugin          string `json:"plugin,omitempty"`      // SS 插件(simple-obfs/v2ray-plugin)
	PluginOpts      string `json:"plugin_opts,omitempty"` // 插件参数原始串("obfs=http;obfs-host=x")
	GrpcServiceName string `json:"grpc_service_name,omitempty"`
	Insecure        bool   `json:"insecure,omitempty"` // 跳过证书校验(订阅里的 insecure=1)
	Region          string `json:"region"`
	Source          string `json:"source"`
	Latency         int    `json:"latency"`
	Available       bool   `json:"available"`
	NodeKey         string `json:"node_key"`
	Blocked         bool   `json:"blocked"`
	Stale           bool   `json:"stale"` // 机场订阅中消失的节点
	// AvailabilitySource 可用性判定来源:never(从未检测)/health(仅健康检查,TCP 快检)/real(真实检测)。
	// 口径由 subscription.Node.AvailabilitySource 统一定义,全池一致(ticket 0016)。
	AvailabilitySource string `json:"availability_source"`
	// DetectionLastCheck 最近一次检测(快检或真实)时间;零值=从未检测,指针省略不与"很久以前"混淆。
	DetectionLastCheck *time.Time `json:"detection_last_check,omitempty"`
	// 最近检测失败原因(ticket 0017):分类为 detection.FailReason* 有限枚举,
	// 详情为截断短文本(不含凭证)。检测成功/从未检测时为空,omitempty 省略键。
	DetectionFailReason string `json:"detection_fail_reason,omitempty"`
	DetectionFailDetail string `json:"detection_fail_detail,omitempty"`
	// 多维解锁检测结果(target_name -> 结果),无检测记录时为 nil
	UnlockResults map[string]unlockResultView `json:"unlock_results,omitempty"`
	// 带宽测试结果（最近一次）
	BandwidthDownMbps float64 `json:"bandwidth_down_mbps,omitempty"`
	BandwidthUpMbps   float64 `json:"bandwidth_up_mbps,omitempty"`
	// 自动标签(从测试结果派生:解锁/出网/质量),无标签时省略
	Tags []string `json:"tags,omitempty"`
	// 最近一次体检的稳定性分(0..100)。指针 + omitempty:无体检记录时省略;
	// 分数 0 是合法的"差"档,指针非空即透出(不与"无分"混淆)。用于稳定性分档筛选(票据 54)。
	StabilityScore *int `json:"stability_score,omitempty"`
}

// toNodeViews 把节点池转换为对外视图列表。blocked 为屏蔽名单，用于标记每个节点是否已被屏蔽。
// unlockResults 为每个节点的多维检测结果(可为 nil,表示不附带)。
// nodeTags 为每个节点的自动标签(可为 nil,表示不附带)。
// stabilityScores 为每个节点最近体检的稳定性分(可为 nil,表示不附带;无该 key 表示无体检记录)。
func toNodeViews(nodes []*subscription.Node, blocked map[string]bool, unlockResults map[string][]store.DetectionResultView, nodeTags map[string][]string, stabilityScores map[string]int) []nodeView {
	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		key := n.NodeKey()
		view := nodeView{
			Name: n.Name, DisplayName: n.DisplayName, Type: n.Type, Region: n.Region,
			Server: n.Server, Port: n.Port, Network: n.Network, TLS: n.TLS, SNI: n.SNI,
			Cipher: n.Cipher, AlterID: n.AlterID,
			Plugin: n.Plugin, PluginOpts: n.PluginOpts,
			GrpcServiceName: n.GrpcServiceName, Insecure: n.Insecure,
			Source: n.Source, Latency: n.Latency, Available: n.Available,
			NodeKey: key, Blocked: blocked[key], Stale: n.Stale,
			AvailabilitySource:  n.AvailabilitySource(),
			DetectionFailReason: n.DetectionFailReason,
			DetectionFailDetail: n.DetectionFailDetail,
			BandwidthDownMbps:   n.BandwidthDownMbps,
			BandwidthUpMbps:     n.BandwidthUpMbps,
		}
		// 最近检测时间:零值(从未检测)留 nil,JSON 省略键
		if !n.DetectionLastCheck.IsZero() {
			t := n.DetectionLastCheck
			view.DetectionLastCheck = &t
		}
		// 附加自动标签(无记录时留空,JSON omitempty 省略)
		if nodeTags != nil {
			view.Tags = nodeTags[key]
		}
		// 附加稳定性分(有体检记录才透出;分数 0 合法,取指针避免 omitempty 吞 0)
		if stabilityScores != nil {
			if score, ok := stabilityScores[key]; ok {
				s := score
				view.StabilityScore = &s
			}
		}
		// 附加多维检测结果
		if unlockResults != nil {
			if results, ok := unlockResults[key]; ok && len(results) > 0 {
				view.UnlockResults = make(map[string]unlockResultView, len(results))
				for _, r := range results {
					view.UnlockResults[r.TargetName] = unlockResultView{
						Available: r.Available,
						Latency:   r.Latency,
						Error:     r.Error,
						DownMbps:  r.DownMbps,
						UpMbps:    r.UpMbps,
						Level:     r.Level,
						Region:    r.Region,
					}
					// 有 connectivity 检测结果时,以它为准更新可用性与延迟显示
					// (统一数据源到 node_health,避免标准化 clone 导致的内存写回丢失)
					if r.TargetName == "connectivity" {
						view.Available = r.Available
						view.Latency = r.Latency
					}
					// bandwidth 结果更新视图带宽字段
					if r.TargetName == "bandwidth" {
						view.BandwidthDownMbps = r.DownMbps
						view.BandwidthUpMbps = r.UpMbps
					}
				}
			}
		}
		views = append(views, view)
	}
	return views
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	// 读屏蔽名单以标记节点状态；读失败降级为空集（节点仍展示，只是都标为未屏蔽）
	blocked, err := s.st.ListBlockedNodes()
	if err != nil {
		s.logger.Warn("list blocked nodes failed", "error", err)
		blocked = map[string]bool{}
	}

	// 管理页展示全量池（含不可用/被屏蔽）并附标准化名，便于原名↔标准名对比（见 ADR 0012/0013）。
	// 管理列表无端点上下文,用全局配置。
	nodes := s.standardizeNodesForEndpoint(s.nodes.Nodes(), nil)
	q := parseNodeQuery(r)
	res := QueryNodes(nodes, blocked, q)

	// 查当前页节点的多维检测结果(只查本页的 node_key,避免全表扫描)
	pageKeys := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		pageKeys = append(pageKeys, n.NodeKey())
	}
	unlockResults, err := s.st.GetLatestDetectionResults(pageKeys)
	if err != nil {
		s.logger.Warn("get detection results failed", "error", err)
		unlockResults = nil // 降级:不附带检测结果,节点仍正常展示
	}

	// 查当前页节点的自动标签(降级为空:不附带标签,节点仍正常展示)
	nodeTags, err := s.st.ListNodeTags(pageKeys)
	if err != nil {
		s.logger.Warn("get node tags failed", "error", err)
		nodeTags = nil
	}

	// 查当前页节点最近体检的稳定性分(供前端稳定性分档筛选;降级为空不阻塞)
	stabilityScores, err := s.st.LatestExamScores(pageKeys)
	if err != nil {
		s.logger.Warn("get exam scores failed", "error", err)
		stabilityScores = nil
	}

	writeJSON(w, map[string]any{
		"last_update": s.nodes.LastUpdate(),
		"nodes":       toNodeViews(res.Nodes, blocked, unlockResults, nodeTags, stabilityScores),
		"total":       res.Total,
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_pages": res.TotalPages,
	})
}

// parseNodeQuery 从请求 query 解析节点分页查询参数（见 ADR 0013）。
func parseNodeQuery(r *http.Request) NodeQuery {
	q := r.URL.Query()
	parseBoolPtr := func(key string) *bool {
		v := q.Get(key)
		if v == "" {
			return nil
		}
		b := v == "true"
		return &b
	}
	parseInt := func(key string, def int) int {
		if n, err := strconv.Atoi(q.Get(key)); err == nil {
			return n
		}
		return def
	}
	return NodeQuery{
		Region:    q.Get("region"),
		Type:      q.Get("type"),
		Available: parseBoolPtr("available"),
		Blocked:   parseBoolPtr("blocked"),
		Stale:     parseBoolPtr("stale"),
		Source:    q.Get("source"),
		SortBy:    q.Get("sort_by"),
		SortOrder: q.Get("sort_order"),
		Page:      parseInt("page", 1),
		PageSize:  parseInt("page_size", DefaultPageSize),
	}
}

// mergeSelfHosted 把当前启用的自建节点合并进节点池，按 NodeKey 去重。
//
// 池中已有的（带上一轮聚合的真实健康状态）优先保留；池中缺失的从 DB 补入
// （ToNode 的占位 Available/Latency，直到下轮聚合健康检查刷新）。这样刚新增的
// 自建节点无需等下一轮刷新即刻出现在订阅/预览里，机场全部拉取失败时它们也仍在。
// 读取失败时降级返回原池——宁可暂缺一次，也不让订阅生成失败。
// 不修改入参底层数组（immutability）：有新增时复制到新切片再追加。
// mergeSelfHosted 把启用的自建节点按"库配置为准 + 池健康覆盖"合并进节点池。
//
// 机场节点原样保留;池中自建节点整体丢弃、仅取其健康状态(Available/Latency/
// LastCheck/DetectionLastCheck)做覆盖来源。自建节点最终由库(ListSelfHostedNodes,
// 仅启用)派生,保证编辑(协议/UUID/…)、禁用、删除都在下次订阅刷新即时生效;
// 若池中有同 NodeKey 的真实健康状态则覆盖占位值。读库失败降级返回原池。
// 不修改入参节点/底层数组(immutability):机场节点按引用保留,自建节点全部新建。
func (s *Server) mergeSelfHosted(nodes []*subscription.Node) []*subscription.Node {
	selfHosted, err := s.st.ListSelfHostedNodes()
	if err != nil {
		s.logger.Warn("list self hosted nodes failed, skipping serve-time merge", "error", err)
		return nodes
	}

	health := make(map[string]*subscription.Node) // NodeKey → 池中旧自建节点(取健康)
	result := make([]*subscription.Node, 0, len(nodes)+len(selfHosted))
	for _, n := range nodes {
		if n.Source == subscription.SourceSelfHosted {
			health[n.NodeKey()] = n // 丢弃旧配置,仅留健康
			continue
		}
		result = append(result, n) // 机场节点原样保留
	}

	for _, shn := range selfHosted {
		node := shn.ToNode() // 库=权威配置
		if h, ok := health[node.NodeKey()]; ok {
			node.Available = h.Available
			node.Latency = h.Latency
			node.LastCheck = h.LastCheck
			node.DetectionLastCheck = h.DetectionLastCheck
			node.DetectionKind = h.DetectionKind // 判定来源随健康状态一并覆盖(ticket 0016)
			// 失败原因与判定来源同生命周期,一并覆盖(ticket 0017)
			node.DetectionFailReason = h.DetectionFailReason
			node.DetectionFailDetail = h.DetectionFailDetail
		}
		result = append(result, node)
	}
	return result
}

// filteredNodes 对节点池应用订阅生成时过滤链（承接 ADR 0005/0009 + 2026-07-15 改动）：
//
//	白名单(非空则只留命中) → 黑名单(剔除命中) → 机场屏蔽(剔除 NodeKey 命中)
//	→ 可用性过滤 → 延迟阈值 → 去重/精选(NodesPerRegion)
//
// 自建节点在每道过滤内部均豁免（FailBack 安全网）。任一数据源读取失败时降级跳过
// 对应过滤——宁可多给节点，也不因设置/名单读不出而让订阅失效。
//
// 注意:节点池现在保留全量数据(含不可用/慢/重复),所有过滤在这里执行,刷新时不再砍节点。
func (s *Server) filteredNodes(nodes []*subscription.Node) []*subscription.Node {
	// serve-time 合并自建节点:填补「新增后到下轮刷新前」的空档,并保证机场全挂时自建节点仍在。
	nodes = s.mergeSelfHosted(nodes)

	settings, err := s.st.GetSystemSettings()
	if err != nil {
		s.logger.Warn("get system settings failed, skipping keyword filters", "error", err)
	} else {
		// 地区白名单过滤（在关键词白名单之前，更精确）
		nodes = s.filterByRegionWhitelist(nodes, settings["region_whitelist"])
		// 关键词白名单/黑名单
		nodes = filter.FilterByWhitelist(nodes, filter.SplitKeywords(settings["filter_whitelist"]))
		nodes = filter.FilterByKeywords(nodes, filter.SplitKeywords(settings["filter_keywords"]))
	}

	blocked, err := s.st.ListBlockedNodes()
	if err != nil {
		s.logger.Warn("list blocked nodes failed, skipping block filter", "error", err)
	} else {
		nodes = filterBlockedNodes(nodes, blocked)
	}

	// 剔除 stale 节点(机场订阅中已消失,保留在池中待清理,但不下发订阅)
	nodes = filterStaleNodes(nodes)

	// 可用性、延迟阈值、去重/精选(刷新时不再过滤,挪到这里执行)
	nodes = filter.FilterAvailable(nodes)
	latencyThreshold := s.cfg.HealthCheck.LatencyThreshold
	nodes = filter.FilterByLatencyThreshold(nodes, latencyThreshold)
	filt := filter.NewFilter(s.cfg.Filter.NodesPerRegion, s.cfg.Filter.Deduplicate)
	nodes = filt.Apply(nodes)

	return nodes
}

// resolveNameConfig 计算节点名称标准化的生效配置（见 ADR 0012）。
//
// 以全局设置为基准（standardize_names / name_template），再按订阅地址覆盖:
// ep.NameMode "on"/"off" 强制开关,ep.NameTemplate 非空则替换模板。ep 为 nil
// 时只取全局（用于无端点上下文的管理列表）。读设置失败时降级为不标准化。
func (s *Server) resolveNameConfig(ep *store.Endpoint) (standardize bool, template string) {
	template = subscription.DefaultNameTemplate
	settings, err := s.st.GetSystemSettings()
	if err != nil {
		s.logger.Warn("get settings failed, skipping name standardization", "error", err)
		return false, template
	}
	standardize = settings["standardize_names"] == "true"
	if t := settings["name_template"]; t != "" {
		template = t
	}

	if ep != nil {
		switch ep.NameMode {
		case store.NameModeOn:
			standardize = true
		case store.NameModeOff:
			standardize = false
		}
		if ep.NameTemplate != "" {
			template = ep.NameTemplate
		}
	}
	return standardize, template
}

// applyStandardization 按解析后的配置对节点池做名称标准化。
//
// standardize=false 时原样返回（各节点 DisplayName 为空，生成器回退用机场原名）。
// 启用时按 (机场,地区) 分组、组内按 NodeKey 排序编号，依 template 生成统一名称。
// 建映射失败时降级为不标准化——宁可用原名，也不让订阅生成失败。
func (s *Server) applyStandardization(nodes []*subscription.Node, standardize bool, template string) []*subscription.Node {
	if !standardize {
		return nodes
	}
	abbrs, err := s.st.AirportAbbreviations()
	if err != nil {
		s.logger.Warn("build airport abbreviations failed, skipping standardization", "error", err)
		return nodes
	}
	// 自建节点无机场简称,注入固定标签 SELF,使其也能套统一模板(见 2026-07-16 设计)
	abbrs[subscription.SourceSelfHosted] = "SELF"
	regions, err := s.st.RegionInfoMap()
	if err != nil {
		s.logger.Warn("build region info failed, skipping standardization", "error", err)
		return nodes
	}
	return subscription.NewStandardizer(template, abbrs, regions).StandardizeNodes(nodes)
}

// standardizeNodesForEndpoint 用订阅地址的生效配置标准化节点池（订阅/预览路径）。
func (s *Server) standardizeNodesForEndpoint(nodes []*subscription.Node, ep *store.Endpoint) []*subscription.Node {
	std, tmpl := s.resolveNameConfig(ep)
	return s.applyStandardization(nodes, std, tmpl)
}

// filterByRegionWhitelist 按地区白名单过滤节点。空白名单=全部通过;非空时只保留白名单地区节点。
// 自建节点豁免（不受白名单约束）。
func (s *Server) filterByRegionWhitelist(nodes []*subscription.Node, whitelistJSON string) []*subscription.Node {
	if whitelistJSON == "" {
		return nodes // 设置项不存在，跳过
	}

	var whitelist []string
	if err := json.Unmarshal([]byte(whitelistJSON), &whitelist); err != nil {
		s.logger.Warn("parse region whitelist failed", "error", err)
		return nodes
	}

	if len(whitelist) == 0 {
		return nodes // 空白名单=全部通过
	}

	allowed := make(map[string]bool)
	for _, region := range whitelist {
		allowed[region] = true
	}

	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		// 自建节点豁免
		if n.Source == subscription.SourceSelfHosted {
			result = append(result, n)
			continue
		}
		// 机场节点按地区过滤
		if allowed[n.Region] {
			result = append(result, n)
		}
	}
	return result
}

// filterBlockedNodes 剔除命中屏蔽名单的机场节点；自建节点豁免。返回新切片，不修改入参。
func filterBlockedNodes(nodes []*subscription.Node, blocked map[string]bool) []*subscription.Node {
	if len(blocked) == 0 {
		return nodes
	}
	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Source == subscription.SourceSelfHosted {
			result = append(result, n)
			continue
		}
		if !blocked[n.NodeKey()] {
			result = append(result, n)
		}
	}
	return result
}

// filterStaleNodes 剔除 stale 节点(机场订阅中已消失的节点)。返回新切片，不修改入参。
// stale 节点保留在池中供后台展示/清理，但不下发到用户订阅。
func filterStaleNodes(nodes []*subscription.Node) []*subscription.Node {
	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		if !n.Stale {
			result = append(result, n)
		}
	}
	return result
}

// renderSubscription 按格式生成订阅内容，返回内容与对应的 Content-Type。
// /sub 与后台预览共用这条链，确保预览所见即所得（见 ADR 0005）。
//
// Clash 格式基于用户可编辑的配置模板渲染（含 hosts/dns/proxy-groups/rules，
// 节点通过 {{nodes}} 占位符动态注入）；V2Ray 格式仍是简单的 base64 链接列表，不走模板。
func (s *Server) renderSubscription(nodes []*subscription.Node, format string) (data []byte, contentType string, err error) {
	switch format {
	case "v2ray":
		data, err = generator.GenerateV2Ray(nodes)
		return data, "text/plain; charset=utf-8", err
	default:
		tmpl, tErr := s.st.GetClashTemplate()
		if tErr != nil {
			return nil, "", fmt.Errorf("load clash template: %w", tErr)
		}
		data, err = generator.RenderTemplate(tmpl, nodes)
		return data, "text/yaml; charset=utf-8", err
	}
}

// handleEndpointPreview 后台预览：对指定订阅地址在“当前那一刻”生成订阅内容与节点清单，
// 应用与 /sub 完全相同的关键词过滤（所见即所得），但不记录拉取统计（见 ADR 0005）。
func (s *Server) handleEndpointPreview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ep, err := s.st.GetEndpointByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	format := r.URL.Query().Get("format")
	if format != "v2ray" {
		format = "clash"
	}

	nodes := s.filteredNodes(s.nodes.Nodes())
	// 与 /sub 同源:预览也套用该端点的节点范围条件与标准化，保证所见即所得（见 ADR 0012 / internal/subfilter）
	nodes = s.applyConditions(nodes, ep)
	nodes = s.standardizeNodesForEndpoint(nodes, ep)

	// 过滤后可能为空（节点池未就绪或全被过滤链剔除）；此时返回空内容而非 500，方便后台排查
	content := ""
	if len(nodes) > 0 {
		data, _, genErr := s.renderSubscription(nodes, format)
		if genErr != nil {
			s.logger.Error("generate preview failed", "format", format, "error", genErr)
			http.Error(w, "generate preview failed", http.StatusInternalServerError)
			return
		}
		content = string(data)
	}

	// 预览明细与 /nodes 同源:附带最新解锁检测结果与自动标签(降级为空不阻塞)
	previewKeys := make([]string, 0, len(nodes))
	for _, n := range nodes {
		previewKeys = append(previewKeys, n.NodeKey())
	}
	unlockResults, err := s.st.GetLatestDetectionResults(previewKeys)
	if err != nil {
		s.logger.Warn("get detection results for preview failed", "error", err)
		unlockResults = nil
	}
	nodeTags, err := s.st.ListNodeTags(previewKeys)
	if err != nil {
		s.logger.Warn("get node tags for preview failed", "error", err)
		nodeTags = nil
	}

	writeJSON(w, map[string]any{
		"format": format,
		"count":  len(nodes),
		// 预览展示的是已过滤后的节点，均未被屏蔽，故传空屏蔽集
		"nodes":   toNodeViews(nodes, nil, unlockResults, nodeTags, nil),
		"content": content,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

// writeJSONStatus 写入带指定状态码的 JSON 响应（用于错误场景回传结构化原因）。
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
