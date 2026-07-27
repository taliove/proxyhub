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
	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/poolops"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
	"github.com/taliove/proxyhub/internal/xraymgr"
)

// NodeSource 节点来源接口（由 Aggregator 实现）
type NodeSource interface {
	Nodes() []*subscription.Node
	// NodesForUser 返回指定用户的节点池(ticket 07 多租户分片)。
	// userID=0 返回全局池(与 Nodes 等价)。
	NodesForUser(userID int64) []*subscription.Node
	LastUpdate() time.Time
	// LastUpdateForUser 返回指定用户池的最近刷新时刻(ticket 07)。
	// userID=0 等价于 LastUpdate()。
	LastUpdateForUser(userID int64) time.Time
	// StartRefreshJob 经 jobs 运行时发起全量刷新任务(trigger 记入 params)。
	// 返回 jobs 行 id/任务 key/是否新启动;同 key 重复触发附加到进行中任务,
	// 与进行中的单机场刷新冲突时返回 aggregator.ErrRefreshConflict。
	StartRefreshJob(trigger string) (jobID int64, key string, started bool, err error)
	// StartRefreshJobForUser 与 StartRefreshJob 同语义,但携带属主 userID(ticket 07):
	// 任务按 userID 分片(同 key 不同用户互不冲突),刷新只聚合该用户名下机场。
	StartRefreshJobForUser(userID int64, trigger string) (jobID int64, key string, started bool, err error)
	// StartAirportRefreshJob 发起单机场刷新(只拉取入池,不含健康检查)。
	// 与全量或同机场的进行中刷新冲突时返回 aggregator.ErrRefreshConflict。
	StartAirportRefreshJob(trigger string, airportID int64) (jobID int64, key string, started bool, err error)
	// StartAirportRefreshJobForUser 与 StartAirportRefreshJob 同语义,但携带属主 userID(ticket 07)。
	StartAirportRefreshJobForUser(userID int64, trigger string, airportID int64) (jobID int64, key string, started bool, err error)
	// CancelRefresh 取消指定 key 的刷新任务;无进行中任务返回 false。
	CancelRefresh(key string) bool
	// CancelRefreshForUser 取消指定用户进行中刷新的任务 key(ticket 07)。
	CancelRefreshForUser(userID int64, key string) bool
	// UpdateNodeTestResult 将单节点即时测试结果写回内存池（按 NodeKey 匹配）。
	// failReason/failDetail 为失败原因分类与短详情(成功时传空串清空,见 ticket 0017)。
	// 找到返回 true；池中无此节点返回 false。
	UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool
	// UpdateNodeTestResultForUser 与 UpdateNodeTestResult 同语义,但写指定用户的池(ticket 07)。
	UpdateNodeTestResultForUser(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool
	// UpdateNodeIdentity 按 NodeKey 更新内存池中节点的身份字段(名称/地区)。
	// name/region 为空表示本次不改该字段。找到返回 true；池中无此节点返回 false。
	UpdateNodeIdentity(nodeKey, name, region string) bool
	// UpdateNodeIdentityForUser 与 UpdateNodeIdentity 同语义,但写指定用户的池(ticket 07)。
	UpdateNodeIdentityForUser(userID int64, nodeKey, name, region string) bool
	// PurgeAirportNodes 一键清空机场节点(内存池+DB 双清,自建豁免,屏蔽/覆盖保留)。
	// 有刷新任务进行中时返回 aggregator.ErrPurgeConflict(拒绝而非等待)。
	PurgeAirportNodes() (int, error)
	// PurgeAirportNodesForUser 与 PurgeAirportNodes 同语义,但只清指定用户的池(ticket 07)。
	PurgeAirportNodesForUser(userID int64) (int, error)
}

// Server HTTP 服务
type Server struct {
	cfg                *config.Config
	st                 *store.Store
	nodes              NodeSource
	webFS              embed.FS
	sessions           *SessionManager
	logger             *slog.Logger
	detectionService   *DetectionService
	detectionJobs      *DetectionServiceJobs
	geo                *geoip.Resolver
	examJobs           *detection.ExamJobManager
	batchExamJobs      *detection.BatchExamJobManager
	stabilityExamJobs  *detection.ExamJobManager
	batchStabilityJobs *detection.BatchStabilityJobManager
	speedtestJobs      *detection.BatchSpeedtestJobManager
	testOrchestrator   *airporttest.Orchestrator
	// airportTestJobs 机场测试任务运行时(kind=airport_test,issue 0025 迁入);
	// 跨 kind 互斥经 airportTestCoordinator(aggregator 实现)协调。
	airportTestJobs *jobs.Manager
	// 订阅现场实测(ADR 0028):检活器与机场测试同源,run 状态只存内存。
	// probeChecker 可在测试中替换为桩实现;写回池直接走 s.nodes(PoolWriter)。
	probeChecker airporttest.HealthChecker
	probeRuns    *probeRunRegistry
	// self-node region resolution seams: real by default, overridable in tests
	// to drive the suggest/save paths without touching DNS or the embedded DB.
	lookupHost    func(host string) ([]string, error)
	countryLookup func(ip string) (string, error)
	// xrayMgr per-user Xray process manager (ticket 08). Wired via
	// SetXrayManager by main; nil in tests that don't exercise the Xray
	// surface. Handlers must nil-check before use.
	xrayMgr *xraymgr.Manager
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
			// 写入侧反查 job id(ADR 0026 样板):回调时任务行仍 running,同 kind+key+属主单实例保证唯一。
			func(userID int64, nodeKey string, report detection.ExamReport) {
				s.onExamComplete(userID, nodeKey, report, s.findRunningJobIDForUser(userID, "exam", nodeKey))
			},
			detection.WithExamJobStore(st.Jobs()),
			detection.WithExamErrorHandler(func(err error) {
				logger.Warn("exam job persistence", "error", err)
			}),
		)

		// 批量体检任务管理器:精简体检(出网 + 稳定性 + 基准下行),游标续跑,
		// 每节点完成回调复用 onExamComplete(落历史 + 触发标签重算)。
		// 批量任务 per-node 回调在 Run 期间触发,任务行必处 running;key 是全局单例常量。
		s.batchExamJobs = detection.NewBatchExamJobManager(
			detectionService.ExamStreamSimplified,
			detectionService.ExamStream,
			func(userID int64, nodeKey string, report detection.ExamReport) {
				s.onExamComplete(userID, nodeKey, report, s.findRunningJobIDForUser(userID, "batch_exam", "batch_exam"))
			},
			detection.WithBatchExamJobStore(st.Jobs()),
			detection.WithBatchExamErrorHandler(func(err error) {
				logger.Warn("batch exam job persistence", "error", err)
			}),
		)

		// 单节点"出网+稳定性"检查任务管理器:与完整体检同构(单发任务/附加回放/取消),
		// kind 名 exam_stability 区分于 exam,避免同一节点两套任务互相附加。
		// 收口复用 onExamComplete:落历史带 source=stability_check 来源标记 + 标签重算。
		s.stabilityExamJobs = detection.NewExamJobManager(
			detectionService.ExamStreamEgressStability,
			func(userID int64, nodeKey string, report detection.ExamReport) {
				s.onExamComplete(userID, nodeKey, report, s.findRunningJobIDForUser(userID, "exam_stability", nodeKey))
			},
			detection.WithExamKindName(detection.ExamStabilityKindName),
			detection.WithExamJobStore(st.Jobs()),
			detection.WithExamErrorHandler(func(err error) {
				logger.Warn("stability exam job persistence", "error", err)
			}),
		)

		// 批量"出网+稳定性"任务管理器:全局单例 + 游标续跑,契约同批量体检。
		s.batchStabilityJobs = detection.NewBatchStabilityJobManager(
			detectionService.ExamStreamEgressStability,
			func(userID int64, nodeKey string, report detection.ExamReport) {
				s.onExamComplete(userID, nodeKey, report, s.findRunningJobIDForUser(userID, "batch_stability", "batch_stability"))
			},
			detection.WithBatchStabilityJobStore(st.Jobs()),
			detection.WithBatchStabilityErrorHandler(func(err error) {
				logger.Warn("batch stability job persistence", "error", err)
			}),
		)

		// 批量快速测速任务管理器:仅基准下行(与体检基准行同口径),游标续跑,
		// 每节点完成回调写回节点视图带宽字段(node_health + 内存池,上行保留)。
		s.speedtestJobs = detection.NewBatchSpeedtestJobManager(
			detectionService.TestBaselineDown,
			s.onSpeedtestComplete,
			detection.WithBatchSpeedtestJobStore(st.Jobs()),
			detection.WithBatchSpeedtestErrorHandler(func(err error) {
				logger.Warn("batch speedtest job persistence", "error", err)
			}),
		)
	}

	// 初始化机场测试编排器
	// 抽样检活复用 healthcheck:直连出口配置热读(与检测主链路同一开关,TUN 下不假通)。
	// 启动即收口上进程残留的进行态测试 run(对齐 FailRunningRefreshRuns 模式与时机:
	// 本进程还没开始跑,任何 diagnosing/checking/scoring 行都是死进程残留)。
	if err := st.FailRunningAirportTestRuns("process restarted"); err != nil {
		logger.Warn("fail stale running airport test runs failed", "error", err)
	}
	storeAdapter := airporttest.NewStoreAdapter(st)
	samplingChecker := healthcheck.NewChecker(
		cfg.HealthCheck.Timeout.Latency,
		cfg.HealthCheck.Timeout.Request,
		cfg.HealthCheck.TestURL,
		cfg.HealthCheck.Concurrent,
	)
	samplingChecker.SetDirectEgressConfigProvider(st.GetDirectEgressConfig)
	healthChecker := NewHealthCheckAdapter(samplingChecker)
	poolOps := poolops.NewStoreAdapter(st)
	s.testOrchestrator = airporttest.NewOrchestratorWithPoolOps(storeAdapter, healthChecker, nodes, poolOps)

	// 机场测试任务运行时(issue 0025:迁入 jobs,ADR 0019 收口):
	// kind 包装 Orchestrator,不可续跑(重启 interrupted);取消=ctx 中断,
	// run 行标 cancelled 且已写回检活结果不回滚。RecoverOwn 不误标其他 Manager 的 kind。
	s.airportTestJobs = jobs.NewManager(
		st.Jobs(),
		jobs.WithErrorHandler(func(err error) {
			logger.Warn("airport test job persistence", "error", err)
		}),
	)
	subFetcher := subscription.NewFetcher(30 * time.Second)
	s.airportTestJobs.Register(airporttest.NewJobKind(
		s.testOrchestrator,
		storeAdapter,
		airporttest.SubscriptionFetch(subFetcher),
		// 写入侧反查 job id(ADR 0026 样板):Run 期间本任务行必 running,同 kind+key 单实例保证唯一。
		func(key string) int64 { return s.findRunningJobID(airporttest.JobKindName, key) },
	))
	if err := s.airportTestJobs.RecoverOwn(); err != nil {
		logger.Error("airport test jobs recover failed", "error", err)
	}
	// 跨 kind 互斥装配:把测试侧 RunningKeys 查询注入刷新侧临界区(aggregator)。
	if c, ok := nodes.(airportTestCoordinator); ok {
		c.SetAirportTestConflictChecker(s.airportTestConflict)
	}

	// 订阅现场实测:复用同一检活器,run 注册表只存内存(带 TTL)
	s.probeChecker = healthChecker
	s.probeRuns = newProbeRunRegistry(endpointProbeRunTTL)

	return s
}

// airportTestCoordinator 跨 kind 互斥协调(aggregator 实现,接口断言装配,issue 0025)。
// 刷新↔测试同机场互斥:两个方向的"冲突检查+发起"共用同一把临界区锁,无 TOCTOU。
type airportTestCoordinator interface {
	// StartAirportTestExclusive 在刷新互斥临界区内查冲突并发起机场测试。
	StartAirportTestExclusive(airportID int64, start func() (int64, string, bool, error)) (int64, string, bool, error)
	// SetAirportTestConflictChecker 注入测试侧进行中任务查询(刷新发起方向用)。
	SetAirportTestConflictChecker(fn func(airportID int64) (string, bool))
}

// airportTestConflict 刷新发起方向的跨 kind 冲突查询:
// airportID=0(全量刷新)任一机场测试在跑即冲突;否则只查同机场 key。
func (s *Server) airportTestConflict(airportID int64) (string, bool) {
	if s.airportTestJobs == nil {
		return "", false
	}
	for _, key := range s.airportTestJobs.RunningKeys(airporttest.JobKindName) {
		if airportID <= 0 || key == airporttest.JobKey(airportID) {
			return key, true
		}
	}
	return "", false
}

// onExamComplete 体检自然完成的收口:落历史(带产出它的 jobs 任务 id,ticket 0022)
// + 按该节点重算自动标签 + 空/Unknown地区回写。
// 三步均为 best-effort:任一失败只记日志,不影响体检结果本身(下一场体检会再算)。
// jobID=0 表示未关联(持久化退化或测试直调),落库后与旧数据同口径,查询侧走时间窗回退。
// userID 为任务属主(ticket 07):写回时按它选择目标池与自建节点表;0 = 全局池。
func (s *Server) onExamComplete(userID int64, nodeKey string, report detection.ExamReport, jobID int64) {
	if err := s.st.SaveExamHistoryWithJobForUser(userID, nodeKey, report, jobID); err != nil {
		s.logger.Warn("save exam history failed", "error", err)
	}
	if err := s.st.RecomputeNodeTags(nodeKey); err != nil {
		s.logger.Warn("recompute node tags after exam failed", "error", err)
	}
	s.writebackRegionIfNeeded(userID, nodeKey, report)
}

// findRunningJobID 反查进行中任务的 jobs 行 id(未归属分片,旧语义)。
func (s *Server) findRunningJobID(kind, key string) int64 {
	return s.findRunningJobIDForUser(0, kind, key)
}

// findRunningJobIDForUser 反查指定属主进行中任务的 jobs 行 id(ADR 0026 写入侧反查样板,
// 参照 aggregator.findRunningJobID):完成回调触发时本任务行仍处 running
// (runJob 中 Finish 后于 OnComplete;批量任务 per-node 回调在 Run 期间),
// 同 kind+key+属主单实例保证唯一;查不到退化 0(查询侧走任务时间窗回退)。
func (s *Server) findRunningJobIDForUser(userID int64, kind, key string) int64 {
	recs, err := s.st.Jobs().LoadRunning()
	if err != nil {
		s.logger.Warn("load running jobs failed, exam history will not link job", "error", err)
		return 0
	}
	for _, r := range recs {
		if r.Kind == kind && r.Key == key && r.UserID == userID {
			return r.ID
		}
	}
	return 0
}

// onSpeedtestComplete 批量快速测速每节点完成的写回:基准下行写回节点视图带宽字段
// (node_health target_name=bandwidth + 内存池),与 handleTestNode 既有写回路径同轨。
// 批量只测下行:上行沿用池内现值写回,避免 up=0 覆盖既有测量(best-effort,失败只记日志)。
// userID 为任务属主(多租户):内存池写回按属主选池。
func (s *Server) onSpeedtestComplete(userID int64, node *subscription.Node, result detection.TestResult) {
	res := result // 局部副本:补上行保留值,不改调用方结果(不可变语义)
	res.UpMbps = s.currentBandwidthUpForUser(userID, node.NodeKey())
	// 可用判定与单节点 bandwidth 档同轨(down+up 双阈值):池内有上行测量(或上行阈值为 0)
	// 时按双阈值重算,仅下行合格不得翻转既有双阈值判定;池内无上行测量时保留批量档
	// 自身的下行判定,不以"缺数据"推翻节点。
	if res.UpMbps > 0 || res.MinUpMbps == 0 {
		res.Available = res.DownMbps >= res.MinDownMbps && res.UpMbps >= res.MinUpMbps
	}
	if err := s.st.SaveTestResult(node.NodeKey(), node.Name, node.Source, res); err != nil {
		s.logger.Warn("save speedtest result failed", "node_key", node.NodeKey(), "error", err)
	}
	s.nodes.UpdateNodeTestResultForUser(userID, node.NodeKey(), "bandwidth", res.Available, res.Latency, res.DownMbps, res.UpMbps, "", "")
}

// currentBandwidthUp 读池内节点当前上行带宽(未归属分片,旧语义)。
func (s *Server) currentBandwidthUp(nodeKey string) float64 {
	return s.currentBandwidthUpForUser(0, nodeKey)
}

// currentBandwidthUpForUser 读指定属主池内节点当前上行带宽(无此节点或池为空返回 0)。
// 从池实时查而非用任务启动时的旁路指针:池写回是不可变替换,旁路指针可能已腐化。
func (s *Server) currentBandwidthUpForUser(userID int64, nodeKey string) float64 {
	if s.nodes == nil {
		return 0
	}
	for _, n := range s.nodes.NodesForUser(userID) {
		if n.NodeKey() == nodeKey {
			return n.BandwidthUpMbps
		}
	}
	return 0
}

// writebackRegionIfNeeded writes back node region from exam egress data.
// Egress country code is the real exit point (ground truth), while GeoIP is just a guess.
// Therefore, when egress data is available, ALWAYS overwrite the node's region with it,
// even if the node already has a non-empty region value (which may be an incorrect GeoIP guess).
// Nodes are updated in memory pool (airport nodes) or database (self-hosted nodes).
// No egress data means no writeback. Best-effort: failures are logged but don't block exam completion.
// userID 为属主(ticket 07):0 = 全局池;非 0 写该用户的池/自建节点表。
func (s *Server) writebackRegionIfNeeded(userID int64, nodeKey string, report detection.ExamReport) {
	// No egress info or no country code - cannot writeback
	if report.Egress == nil || report.Egress.IPv4 == nil || report.Egress.IPv4.CountryCode == "" {
		return
	}

	countryCode := report.Egress.IPv4.CountryCode

	// Try updating airport node (memory pool)
	if s.updateAirportNodeRegion(userID, nodeKey, countryCode) {
		return
	}

	// Try updating self-hosted node (database)
	if err := s.updateSelfHostedNodeRegion(userID, nodeKey, countryCode); err != nil {
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
// userID 为属主(ticket 07):0 = 全局池;非 0 写该用户的池。
func (s *Server) updateAirportNodeRegion(userID int64, nodeKey, countryCode string) bool {
	if s.nodes == nil {
		return false
	}
	for _, n := range s.nodes.NodesForUser(userID) {
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
// 按属主(ticket 07)查表与更新,防止跨用户写串。
func (s *Server) updateSelfHostedNodeRegion(userID int64, nodeKey, countryCode string) error {
	nodes, err := s.st.ListAllSelfHostedNodesByUser(userID)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.ToNode().NodeKey() != nodeKey {
			continue
		}
		// Always overwrite region with egress country code
		n.RegionCode = countryCode
		if err := s.st.UpdateSelfHostedNodeForUser(userID, n); err != nil {
			return err
		}
		// Sync memory pool region so /nodes reflects egress truth immediately
		// (ticket 47); name unchanged here, pass empty to leave it.
		s.syncSelfHostedNodeIdentityForUser(userID, nodeKey, "", countryCode)
		return nil
	}
	return store.ErrNotFound
}

// syncSelfHostedNodeIdentity 兼容旧签名(ticket 07 前):写全局池(userID=0)。
func (s *Server) syncSelfHostedNodeIdentity(nodeKey, name, regionCode string) {
	s.syncSelfHostedNodeIdentityForUser(0, nodeKey, name, regionCode)
}

// syncSelfHostedNodeIdentityForUser pushes a self-hosted node's new name/region into the
// memory pool by NodeKey, so the admin /nodes list reflects renames and region
// writebacks immediately instead of waiting for the next aggregation refresh
// (ticket 47). name/region empty means "leave that field". No-op if the pool has
// no such node (self-hosted node not yet merged into the pool).
// userID 为属主(ticket 07):写该用户的池;0 = 全局池。
func (s *Server) syncSelfHostedNodeIdentityForUser(userID int64, nodeKey, name, regionCode string) {
	if s.nodes == nil {
		return
	}
	s.nodes.UpdateNodeIdentityForUser(userID, nodeKey, name, regionCode)
}

// RecoverJobs 重启恢复:把上次进程遗留的 running 任务恢复或标记中断。
// 单发任务(exam/exam_stability)标记 interrupted,批量任务(batch_exam/batch_stability/batch_speedtest)从游标续跑。
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
	if s.stabilityExamJobs != nil {
		if err := s.stabilityExamJobs.RecoverInterrupted(); err != nil {
			s.logger.Warn("recover interrupted stability exam jobs failed", "error", err)
		}
	}
	if s.batchStabilityJobs != nil {
		if err := s.batchStabilityJobs.RecoverInterrupted(); err != nil {
			s.logger.Warn("recover interrupted batch stability jobs failed", "error", err)
		}
	}
	if s.speedtestJobs != nil {
		if err := s.speedtestJobs.RecoverInterrupted(); err != nil {
			s.logger.Warn("recover interrupted batch speedtest jobs failed", "error", err)
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

// SetXrayManager 注入每用户 Xray 进程管理器(main 接线;ticket 08)。
// 幂等 setter,未接线时 Xray handler 返回 503。
func (s *Server) SetXrayManager(m *xraymgr.Manager) {
	s.xrayMgr = m
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
			if s.stabilityExamJobs != nil {
				s.stabilityExamJobs.SweepExpired()
			}
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

	// 订阅地址管理(业务路由统一走 requirePasswordChanged,首登强改密拦截)
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return s.requireAuth(s.requirePasswordChanged(h))
	}
	// 超管专属路由链:requireAuth + requirePasswordChanged + requireAdmin。普通用户 403;
	// 首登未改密的超管同样先 403 去改密(改密入口 /api/me/password 已豁免,无锁死)。
	// 提前声明:安全审计/批量解锁检测等超管面在路由表前段即引用。
	adminGuard := func(h http.HandlerFunc) http.HandlerFunc {
		return s.requireAuth(s.requirePasswordChanged(s.requireAdmin(h)))
	}
	// 首登强改密的豁免面:must_change_password 会话被挡在业务路由外,但
	// 读自身状态、改自己密码、登出必须可达,否则改密接口把自己锁死。
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/me/password", s.requireAuth(s.handleChangeMyPassword))
	mux.HandleFunc("GET /api/endpoints", guard(s.handleListEndpoints))
	mux.HandleFunc("POST /api/endpoints", guard(s.handleCreateEndpoint))
	mux.HandleFunc("POST /api/endpoints/{id}/toggle", guard(s.handleToggleEndpoint))
	mux.HandleFunc("PUT /api/endpoints/{id}/name-config", guard(s.handleUpdateEndpointNameConfig))
	mux.HandleFunc("PUT /api/endpoints/{id}/template", guard(s.handleUpdateEndpointTemplate))
	mux.HandleFunc("PUT /api/endpoints/{id}/conditions", guard(s.handleUpdateEndpointConditions))
	mux.HandleFunc("POST /api/endpoints/preview-conditions", guard(s.handlePreviewConditions))
	mux.HandleFunc("DELETE /api/endpoints/{id}", guard(s.handleDeleteEndpoint))
	mux.HandleFunc("GET /api/endpoints/{id}/stats", guard(s.handleEndpointStats))
	mux.HandleFunc("GET /api/endpoints/{id}/preview", guard(s.handleEndpointPreview))
	mux.HandleFunc("POST /api/endpoints/{id}/test", guard(s.handleEndpointTest))
	mux.HandleFunc("POST /api/endpoints/{id}/test/probe", guard(s.handleEndpointTestProbe))
	mux.HandleFunc("GET /api/endpoints/{id}/test/probe/{runId}", guard(s.handleGetEndpointTestProbe))

	// 机场管理
	mux.HandleFunc("GET /api/airports", guard(s.handleListAirports))
	mux.HandleFunc("GET /api/airports/abbr-suggest", guard(s.handleSuggestAbbr))
	mux.HandleFunc("POST /api/airports", guard(s.handleCreateAirport))
	mux.HandleFunc("PUT /api/airports/{id}", guard(s.handleUpdateAirport))
	mux.HandleFunc("POST /api/airports/{id}/toggle", guard(s.handleToggleAirport))
	mux.HandleFunc("POST /api/airports/{id}/refresh", guard(s.handleAirportRefresh))
	mux.HandleFunc("DELETE /api/airports/{id}", guard(s.handleDeleteAirport))

	// 机场测试（诊断+抽样检活+评分）
	mux.HandleFunc("POST /api/airports/{id}/test", guard(s.handleAirportTest))
	mux.HandleFunc("GET /api/airports/{id}/test/runs/{runId}", guard(s.handleGetAirportTestRun))
	mux.HandleFunc("GET /api/airports/{id}/test/runs", guard(s.handleListAirportTestRuns))

	// 节点状态
	mux.HandleFunc("GET /api/nodes", guard(s.handleListNodes))
	// 节点分享 URI 生成（用于生成二维码）
	mux.HandleFunc("GET /api/nodes/{nodeKey}/share-uri", guard(s.handleNodeShareURI))

	// 机场节点屏蔽（按 NodeKey 精确拉黑，跨刷新持久）
	mux.HandleFunc("POST /api/nodes/block", guard(s.handleBlockNode))
	mux.HandleFunc("POST /api/nodes/unblock", guard(s.handleUnblockNode))

	// 批量屏蔽：按机场（source）或显式 node_keys 一次性拉黑/取消
	mux.HandleFunc("POST /api/nodes/batch-block", guard(s.handleBatchBlockNodes))
	mux.HandleFunc("POST /api/nodes/batch-unblock", guard(s.handleBatchUnblockNodes))

	// 批量刷新名称：重跑地区识别+标准化
	mux.HandleFunc("POST /api/nodes/refresh-names", guard(s.handleRefreshNames))

	// 自建节点管理（增/改/删/启停）
	mux.HandleFunc("GET /api/self-nodes", guard(s.handleListSelfNodes))
	mux.HandleFunc("GET /api/self-nodes/suggest", guard(s.handleSuggestSelfNode))
	mux.HandleFunc("POST /api/self-nodes", guard(s.handleCreateSelfNode))
	mux.HandleFunc("PUT /api/self-nodes/{id}", guard(s.handleUpdateSelfNode))
	mux.HandleFunc("DELETE /api/self-nodes/{id}", guard(s.handleDeleteSelfNode))
	mux.HandleFunc("POST /api/self-nodes/{id}/toggle", guard(s.handleToggleSelfNode))

	// 聚合器手动刷新
	mux.HandleFunc("POST /api/aggregator/refresh", guard(s.handleManualRefresh))

	// 刷新历史
	mux.HandleFunc("GET /api/refresh/runs", guard(s.handleListRefreshRuns))
	mux.HandleFunc("GET /api/refresh/runs/{id}", guard(s.handleGetRefreshRun))

	// 节点解锁检测(批量,全局单例内存态 + 遍历合并池):超管专属。
	// 按用户分片的收益低(结果按 node_key 全局共享)而重构成本高(spec-multi-tenant-isolation 遗留待决 3 的落地决策)。
	mux.HandleFunc("POST /api/detection/trigger", adminGuard(s.handleTriggerDetection))
	mux.HandleFunc("POST /api/detection/cancel", adminGuard(s.handleCancelDetection))
	mux.HandleFunc("GET /api/detection/status", adminGuard(s.handleDetectionStatus))

	// 通用任务 API(任务中心)
	mux.HandleFunc("GET /api/jobs", guard(s.handleListJobs))
	mux.HandleFunc("GET /api/jobs/{id}", guard(s.handleGetJobDetail))
	mux.HandleFunc("GET /api/jobs/{id}/result", guard(s.handleGetJobResult))
	mux.HandleFunc("POST /api/jobs/{kind}/{key}/cancel", guard(s.handleCancelJob))
	mux.HandleFunc("POST /api/nodes/test", guard(s.handleTestNode))
	mux.HandleFunc("GET /api/nodes/test/stream", guard(s.handleTestNodeStream))
	mux.HandleFunc("GET /api/nodes/exam/stream", guard(s.handleNodeExamStream))
	mux.HandleFunc("POST /api/nodes/exam/cancel", guard(s.handleNodeExamCancel))
	mux.HandleFunc("GET /api/nodes/exam/latest", guard(s.handleGetExamLatest))
	mux.HandleFunc("GET /api/nodes/exam/history", guard(s.handleGetExamHistory))
	mux.HandleFunc("POST /api/nodes/exam/batch", guard(s.handleBatchExam))
	mux.HandleFunc("GET /api/nodes/exam/batch/stream", guard(s.handleBatchExamStream))
	mux.HandleFunc("POST /api/nodes/exam/batch/cancel", guard(s.handleBatchExamCancel))
	// 出网+稳定性检查(动作2):批量形态(任务化) + 单节点形态(行内 SSE)
	mux.HandleFunc("GET /api/nodes/stability/stream", guard(s.handleNodeStabilityStream))
	mux.HandleFunc("POST /api/nodes/stability/cancel", guard(s.handleNodeStabilityCancel))
	mux.HandleFunc("POST /api/nodes/stability/batch", guard(s.handleBatchStability))
	mux.HandleFunc("GET /api/nodes/stability/batch/stream", guard(s.handleBatchStabilityStream))
	mux.HandleFunc("POST /api/nodes/stability/batch/cancel", guard(s.handleBatchStabilityCancel))

	// 快速测速(动作3):批量形态(任务化,仅基准下行)
	mux.HandleFunc("POST /api/nodes/speedtest/batch", guard(s.handleBatchSpeedtest))
	mux.HandleFunc("GET /api/nodes/speedtest/batch/stream", guard(s.handleBatchSpeedtestStream))
	mux.HandleFunc("POST /api/nodes/speedtest/batch/cancel", guard(s.handleBatchSpeedtestCancel))
	// 节点管理（覆盖层 + 清理）
	mux.HandleFunc("PUT /api/nodes/override", guard(s.handleSetNodeOverride))
	mux.HandleFunc("DELETE /api/nodes/override", guard(s.handleClearNodeOverride))
	mux.HandleFunc("POST /api/nodes/cleanup", guard(s.handleCleanupNodes))
	mux.HandleFunc("POST /api/nodes/purge-airport", guard(s.handlePurgeAirportNodes))

	// 系统设置
	mux.HandleFunc("GET /api/settings", guard(s.handleGetSettings))
	mux.HandleFunc("POST /api/settings", guard(s.handleSaveSettings))

	// 检测目标配置
	mux.HandleFunc("GET /api/settings/detection-targets", guard(s.handleGetDetectionTargets))
	mux.HandleFunc("PUT /api/settings/detection-targets", guard(s.handleSaveDetectionTargets))

	// 晚间标签重算调度配置(schedule_retag_time / schedule_retag_enabled):
	// 全局单例调度器 + 全局共享标签数据,超管专属(见 CONTEXT.md「租户级设置」)。
	mux.HandleFunc("GET /api/settings/schedule", adminGuard(s.handleGetSchedule))
	mux.HandleFunc("PUT /api/settings/schedule", adminGuard(s.handleSaveSchedule))

	// 地区白名单
	mux.HandleFunc("GET /api/settings/region-whitelist", guard(s.handleGetRegionWhitelist))
	mux.HandleFunc("POST /api/settings/region-whitelist", guard(s.handleSetRegionWhitelist))
	mux.HandleFunc("GET /api/settings/regions", guard(s.handleListRegions))

	// 配置模板（Clash 订阅骨架，含 hosts/dns/proxy-groups/rules）
	mux.HandleFunc("GET /api/settings/template", guard(s.handleGetTemplate))
	mux.HandleFunc("PUT /api/settings/template", guard(s.handleSaveTemplate))
	mux.HandleFunc("POST /api/settings/template/reset", guard(s.handleResetTemplate))

	// 模板库(ticket endpoint-template-01):用户级多模板,每用户可创建多套命名模板
	mux.HandleFunc("GET /api/templates", guard(s.handleListTemplates))
	mux.HandleFunc("POST /api/templates", guard(s.handleCreateTemplate))
	mux.HandleFunc("GET /api/templates/{name}", guard(s.handleGetTemplateByName))
	mux.HandleFunc("PUT /api/templates/{name}", guard(s.handleUpdateTemplate))
	mux.HandleFunc("DELETE /api/templates/{name}", guard(s.handleDeleteTemplate))
	mux.HandleFunc("PUT /api/templates/{name}/default", guard(s.handleSetDefaultTemplate))
	// 模板版本历史(ticket template-editor-upgrade-01):每次落库自动追加版本,保留最近 20 个
	mux.HandleFunc("GET /api/templates/{name}/versions", guard(s.handleListVersions))
	mux.HandleFunc("GET /api/templates/{name}/versions/{version}", guard(s.handleGetVersionContent))

	// 仪表盘统计 + 优质节点聚合
	mux.HandleFunc("GET /api/dashboard/stats", guard(s.handleDashboardStats))
	mux.HandleFunc("GET /api/dashboard/top-nodes", guard(s.handleDashboardTopNodes))

	// 访问统计（全局汇总 + 拉取趋势）
	mux.HandleFunc("GET /api/stats/global", guard(s.handleGlobalStats))
	mux.HandleFunc("GET /api/stats/trend", guard(s.handlePullTrend))

	// 本机实测（浏览器端测速,入站服务,与检测出站链路无关;ticket 0032）:
	// 下行发流是带宽放大器,全部过 requireAuth,不挂公开面
	mux.HandleFunc("GET /api/speedtest/ping", guard(s.handleSpeedtestPing))
	mux.HandleFunc("GET /api/speedtest/download", guard(s.handleSpeedtestDownload))
	mux.HandleFunc("POST /api/speedtest/upload", guard(s.handleSpeedtestUpload))
	// 本机实测透传(经选定节点访问 Cloudflare,流式转发给浏览器):流量经浏览器可见大 Size,
	// 且经用户下拉选定节点(不依赖客户端代理选择)。8 并行 fetch 聚合带宽。
	mux.HandleFunc("GET /api/speedtest/proxy-download/stream", guard(s.handleSpeedtestProxyDownload))
	mux.HandleFunc("GET /api/speedtest/proxy-latency", guard(s.handleSpeedtestProxyLatency))
	mux.HandleFunc("POST /api/speedtest/proxy-upload/stream", guard(s.handleSpeedtestProxyUpload))
	mux.HandleFunc("POST /api/speedtest/results", guard(s.handleSaveSpeedtestResult))
	mux.HandleFunc("GET /api/speedtest/results", guard(s.handleListSpeedtestResults))
	mux.HandleFunc("DELETE /api/speedtest/results/{id}", guard(s.handleDeleteSpeedtestResult))

	// 每用户 Xray 实例(ticket 08):用户读自己的实例;超管读/重启任意用户实例
	mux.HandleFunc("GET /api/me/xray", guard(s.handleGetMyXray))
	mux.HandleFunc("GET /api/admin/users/{id}/xray", guard(s.handleGetUserXray))
	mux.HandleFunc("POST /api/admin/users/{id}/xray/restart", guard(s.handleRestartUserXray))

	// 用户管理(ticket 03,超管专属):列表/创建/详情/修改/启停/删除/重置密码。
	// 走 requireAuth + requirePasswordChanged + requireAdmin 链(adminGuard 见路由表前段)。
	mux.HandleFunc("GET /api/admin/users", adminGuard(s.handleAdminListUsers))
	mux.HandleFunc("POST /api/admin/users", adminGuard(s.handleAdminCreateUser))
	mux.HandleFunc("GET /api/admin/users/{id}", adminGuard(s.handleAdminGetUser))
	mux.HandleFunc("PUT /api/admin/users/{id}", adminGuard(s.handleAdminUpdateUser))
	mux.HandleFunc("POST /api/admin/users/{id}/disable", adminGuard(s.handleAdminDisableUser))
	mux.HandleFunc("POST /api/admin/users/{id}/enable", adminGuard(s.handleAdminEnableUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", adminGuard(s.handleAdminDeleteUser))
	mux.HandleFunc("POST /api/admin/users/{id}/reset-password", adminGuard(s.handleAdminResetPassword))

	// 安全审计(登录/封禁事件流水 + 当前封禁 IP 管理):超管专属。
	// 含写操作(解封 IP),普通用户可达即越权。
	mux.HandleFunc("GET /api/audit/events", adminGuard(s.handleAuditEvents))
	mux.HandleFunc("GET /api/audit/banned", adminGuard(s.handleBannedIPs))
	mux.HandleFunc("POST /api/audit/unban", adminGuard(s.handleUnbanIP))

	// 超管视角切换(ticket 09):进入/退出用户空间,查询当前生效视角。
	// 持久化在 session 上;之后所有请求经 requireAuth 自动套用 acting target。
	mux.HandleFunc("POST /api/admin/switch-user", adminGuard(s.handleAdminSwitchUser))
	mux.HandleFunc("POST /api/admin/exit-switch", adminGuard(s.handleAdminExitSwitch))
	mux.HandleFunc("GET /api/admin/current-view", adminGuard(s.handleAdminCurrentView))

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

// clientIP 提取客户端 IP。仅当对端是受信反代(loopback)时才采信
// X-Forwarded-For / X-Real-IP;否则直连客户端可伪造 XFF 永久豁免
// IP2Ban 与蜜罐判定(go-reviewer M5)。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
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

	// 订阅时过滤链：白名单 → 黑名单 → 机场屏蔽，自建节点全程豁免（见 ADR 0005/0009）。
	// 注意：不在此处对空池提前 503——filteredNodes 内含 serve-time 合并自建节点,
	// 全新装机仅配置自建节点时也必须能拉到订阅(与订阅测试口径一致,见 ADR 0028 决策 1)。
	// 订阅端点:按端点属主选择节点池与自建节点(ticket 07;属主 0 = 全局池)。
	nodes := s.filteredNodes(s.nodes.NodesForUser(ep.UserID), ep.UserID)
	// 该订阅地址的节点范围条件(动态查询,见 internal/subfilter);空条件=全量(零回归)
	nodes = s.applyConditions(nodes, ep)
	if len(nodes) == 0 {
		// 过滤链过窄把节点池清空：与池未就绪同样返回 503，避免生成空订阅落到 500，
		// 也与后台预览（返回空清单）保持一致
		http.Error(w, "no available nodes yet, try again later", http.StatusServiceUnavailable)
		return
	}

	// 节点名称标准化（见 ADR 0012）：按该订阅地址的生效配置（端点覆盖 → 全局回退）统一名称。
	nodes = s.standardizeNodesForEndpoint(nodes, ep, ep.UserID)

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

	data, contentType, err := s.renderSubscriptionForEndpoint(nodes, format, ep)
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

	user, verr := s.verifyCredentials(req.Username, req.Password)
	if verr != nil {
		if errors.Is(verr, errAccountDisabled) {
			// 已禁用账号:不再计入 IP 失败阈值(凭据本身是对的,只是账号被关),
			// 直接 403,让管理员排障时区分"密码错"与"账号被禁用"。
			s.recordAudit("login_disabled", ip, req.Username, "")
			http.Error(w, "account disabled", http.StatusForbidden)
			return
		}
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

	// 登录成功：清空失败计数，更新 last_login_at,发放携带身份载荷的会话
	s.st.ResetLoginFailures(ip)
	s.recordAudit("login_success", ip, req.Username, "")
	now := time.Now()
	if err := s.st.UpdateUser(user.ID, store.UserUpdate{LastLoginAt: &now}); err != nil {
		// 登录动作本身已经成功,只记录日志不打断
		s.logger.Warn("update last_login_at failed", "user_id", user.ID, "error", err)
	}
	token, err := s.sessions.CreateWithPayload(SessionPayload{
		UserID:             user.ID,
		Role:               user.Role,
		MustChangePassword: user.MustChangePassword,
	})
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
	writeJSON(w, map[string]any{
		"ok": true,
		"user": map[string]any{
			"id":                   user.ID,
			"username":             user.Username,
			"role":                 user.Role,
			"must_change_password": user.MustChangePassword,
		},
	})
}

// errAccountDisabled 表示凭据匹配但账号已被禁用(disabled_at 非空)。
var errAccountDisabled = errors.New("account disabled")

// verifyCredentials 按 users 表校验用户名和密码(bcrypt)。
// 成功返回用户记录;用户名不存在/密码错/用户已禁用分别返回不同错误,
// 其中禁用走 errAccountDisabled,其余统一当作"凭据无效"。
// 未知用户也会执行一次 bcrypt 比较以均匀化时序,避免用户名枚举。
func (s *Server) verifyCredentials(username, password string) (*store.User, error) {
	// 固定 dummy 哈希,在用户不存在时用作比较对象,使响应时间接近真实校验。
	// 值本身无意义,仅占用 CPU,防止通过时序探测用户名是否存在。
	const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

	user, err := s.st.GetUserByUsername(username)
	if err != nil {
		// 不存在或其他错误:走 dummy 比较再统一报"凭据无效"。
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return nil, errors.New("invalid credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(password)) != nil {
		return nil, errors.New("invalid credentials")
	}
	if user.Disabled() {
		return nil, errAccountDisabled
	}
	return user, nil
}

// requireAuth 会话鉴权中间件:校验 session,解析用户身份注入 request context(ticket 07)。
// 载荷解析规则:
//   - 新会话(ticket 02 风格,带 UserID/Role)直接取用;
//   - 旧会话(settings-KV 时代)回退为首个 super_admin(单管理员部署等价语义);
//   - 超管视角切换走 POST /api/admin/switch-user(ticket 09),持久化到会话
//         acting_user_id,之后请求免传。
//
// 在线再校验(go-reviewer H1/H2):非旧会话每请求重读 users 表——用户被删 401、
// 被禁用 401、角色以库为准刷新进 scope(降级即刻生效)。读库失败(非 NotFound)
// 降级放行并用会话声明,避免一次 DB 抖动把全员锁在门外(与 requirePasswordChanged
// 同一策略)。
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		payload, ok := s.sessions.Lookup(cookie.Value)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		scope := UserScope{
			UserID:       payload.UserID,
			Role:         payload.Role,
			ActingUserID: payload.ActingUserID,
		}
		if payload.Legacy {
			// 旧会话无身份载荷:单管理员时代唯一可解析身份是首个 super_admin。
			legacyID, lerr := s.firstSuperAdminID()
			if lerr != nil {
				s.logger.Error("resolve legacy session user failed", "error", lerr)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			scope.UserID = legacyID
			scope.Role = store.RoleSuperAdmin
		} else {
			user, uerr := s.st.GetUserByID(payload.UserID)
			switch {
			case errors.Is(uerr, store.ErrNotFound):
				// 用户已被删除:会话即刻失效。
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			case uerr != nil:
				s.logger.Warn("re-validate session user failed, allowing with session claims",
					"user_id", payload.UserID, "error", uerr)
			case user.Disabled():
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			default:
				scope.Role = user.Role
			}
		}

		next(w, r.WithContext(ContextWithUserScope(r.Context(), scope)))
	}
}

// firstSuperAdminID 返回首个 super_admin 的用户 id(旧会话身份回退用)。
// 无用户(初始化前)返回错误。
func (s *Server) firstSuperAdminID() (int64, error) {
	users, err := s.st.ListUsers()
	if err != nil {
		return 0, err
	}
	for _, u := range users {
		if u.Role == store.RoleSuperAdmin {
			return u.ID, nil
		}
	}
	return 0, errors.New("no super admin provisioned")
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

// handleMe 返回当前登录用户的完整 profile(users 表 + 配额)。
// 视图口径:超管处于 impersonate 视角时返回被视角用户的信息,便于前端展示
// "当前正在以谁的身份操作";普通用户永远只能看到自己。
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effectiveID := EffectiveUserID(scope)

	user, err := s.st.GetUserByID(effectiveID)
	if err != nil {
		// 会话指向的用户被删了,等同于登录态失效
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.logger.Error("get user failed", "user_id", effectiveID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"id":                   user.ID,
		"username":             user.Username,
		"role":                 user.Role,
		"must_change_password": user.MustChangePassword,
	}
	// 配额是可选附属:无记录视为"未配置",不视为错误
	if quota, qerr := s.st.GetUserQuota(effectiveID); qerr == nil {
		resp["quota"] = quota
	} else if !errors.Is(qerr, store.ErrNotFound) {
		s.logger.Warn("get user quota failed", "user_id", effectiveID, "error", qerr)
	}
	// 标注视角,前端可据此显示"impersonating as X"
	if scope.IsSuperAdmin() && scope.ActingUserID > 0 && scope.ActingUserID != scope.UserID {
		resp["acting"] = true
	}
	writeJSON(w, resp)
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	eps, err := s.st.ListEndpointsByUser(effUID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// 可用性汇总加性附加(ADR 0028 决策 2):池在内存,全局过滤链只跑一遍,
	// 各端点仅条件维度不同,逐个叠加计算。
	base := s.filteredNodes(s.nodes.NodesForUser(effUID), effUID)
	items := make([]endpointListItem, 0, len(eps))
	for _, ep := range eps {
		items = append(items, endpointListItem{
			Endpoint:     ep,
			Availability: s.availabilityFor(base, ep),
		})
	}
	writeJSON(w, items)
}

func (s *Server) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	var req struct {
		Alias        string `json:"alias"`
		TemplateName string `json:"template_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Alias) == "" {
		http.Error(w, "alias is required", http.StatusBadRequest)
		return
	}

	effUID := EffectiveUserID(scope)

	// Validate template exists if specified (fail-fast)
	if req.TemplateName != "" {
		_, err := s.st.GetTemplateByName(effUID, req.TemplateName)
		if err != nil {
			http.Error(w, "template not found", http.StatusBadRequest)
			return
		}
	}

	ep, err := s.st.CreateEndpointForUser(effUID, strings.TrimSpace(req.Alias))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Bind template if specified
	if req.TemplateName != "" {
		if err := s.st.UpdateEndpointTemplate(effUID, ep.ID, req.TemplateName); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Reload to get updated template_name
		ep, _ = s.st.GetEndpointByIDForUser(effUID, ep.ID)
	}

	writeJSON(w, ep)
}

func (s *Server) handleToggleEndpoint(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	effUID := EffectiveUserID(scope)
	ep, err := s.st.GetEndpointByIDForUser(effUID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := s.st.SetEndpointEnabledForUser(effUID, id, !ep.Enabled); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"enabled": !ep.Enabled})
}

// handleUpdateEndpointNameConfig 设置订阅地址的节点名称标准化覆盖（见 ADR 0012）。
func (s *Server) handleUpdateEndpointNameConfig(w http.ResponseWriter, r *http.Request) {
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
		NameMode     string `json:"name_mode"`
		NameTemplate string `json:"name_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := s.st.UpdateEndpointNameConfigForUser(EffectiveUserID(scope), id, req.NameMode, req.NameTemplate); err != nil {
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

// handleUpdateEndpointTemplate binds or clears an endpoint's template reference (ticket endpoint-template-02).
func (s *Server) handleUpdateEndpointTemplate(w http.ResponseWriter, r *http.Request) {
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
		TemplateName string `json:"template_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	effUID := EffectiveUserID(scope)

	// Verify endpoint exists and user owns it
	_, err = s.st.GetEndpointByIDForUser(effUID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Update template binding (validates template exists if non-empty)
	if err := s.st.UpdateEndpointTemplate(effUID, id, req.TemplateName); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusBadRequest)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

func (s *Server) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.st.DeleteEndpointForUser(EffectiveUserID(scope), id); err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleEndpointStats(w http.ResponseWriter, r *http.Request) {
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
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	// 读本人屏蔽名单以标记节点状态；读失败降级为空集（节点仍展示，只是都标为未屏蔽）
	blocked, err := s.st.ListBlockedNodesForUser(effUID)
	if err != nil {
		s.logger.Warn("list blocked nodes failed", "error", err)
		blocked = map[string]bool{}
	}

	// 管理页展示全量池（含不可用/被屏蔽）并附标准化名，便于原名↔标准名对比（见 ADR 0012/0013）。
	// 管理列表无端点上下文,用全局配置。
	nodes := s.standardizeNodesForEndpoint(s.nodes.NodesForUser(effUID), nil, effUID)
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

	// 查当前页节点最近体检的稳定性分(完整体检口径:排除"出网+稳定性"任务的缺段报告;
	// 降级为空不阻塞)
	stabilityScores, err := s.st.LatestCompleteExamScores(pageKeys)
	if err != nil {
		s.logger.Warn("get exam scores failed", "error", err)
		stabilityScores = nil
	}

	writeJSON(w, map[string]any{
		"last_update": s.nodes.LastUpdateForUser(effUID),
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
		Keyword:   q.Get("keyword"),
		SortBy:    q.Get("sort_by"),
		SortOrder: q.Get("sort_order"),
		Page:      parseInt("page", 1),
		PageSize:  parseInt("page_size", DefaultPageSize),
	}
}

// mergeSelfHosted 把启用的自建节点按"库配置为准 + 池健康覆盖"合并进节点池。
//
// 机场节点原样保留;池中自建节点整体丢弃、仅取其健康状态(Available/Latency/
// LastCheck/DetectionLastCheck)做覆盖来源。自建节点最终由库(ListSelfHostedNodesByUser,
// 仅启用)派生,保证编辑(协议/UUID/…)、禁用、删除都在下次订阅刷新即时生效;
// 若池中有同 NodeKey 的真实健康状态则覆盖占位值。读库失败降级返回原池。
// 不修改入参节点/底层数组(immutability):机场节点按引用保留,自建节点全部新建。
// userID 为属主(ticket 07):0 = 全局池(不校验属主)。
func (s *Server) mergeSelfHosted(nodes []*subscription.Node, userID int64) []*subscription.Node {
	selfHosted, err := s.st.ListSelfHostedNodesByUser(userID)
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
// userID 为属主(ticket 07):0 = 全局池,非 0 时自建节点合并只读该用户的。
func (s *Server) filteredNodes(nodes []*subscription.Node, userID int64) []*subscription.Node {
	// serve-time 合并自建节点:填补「新增后到下轮刷新前」的空档,并保证机场全挂时自建节点仍在。
	nodes = s.mergeSelfHosted(nodes, userID)

	// 过滤链三键按属主读取(租户级设置,回退全局默认);读取失败降级跳过对应过滤。
	if wl, err := s.st.GetSettingForUser(userID, "region_whitelist"); err == nil {
		nodes = s.filterByRegionWhitelist(nodes, wl)
	} else {
		s.logger.Warn("get region whitelist failed, skipping region filter", "error", err)
	}
	if kw, err := s.st.GetSettingForUser(userID, "filter_whitelist"); err == nil {
		nodes = filter.FilterByWhitelist(nodes, filter.SplitKeywords(kw))
	} else {
		s.logger.Warn("get filter whitelist failed, skipping keyword whitelist", "error", err)
	}
	if kw, err := s.st.GetSettingForUser(userID, "filter_keywords"); err == nil {
		nodes = filter.FilterByKeywords(nodes, filter.SplitKeywords(kw))
	} else {
		s.logger.Warn("get filter keywords failed, skipping keyword blacklist", "error", err)
	}

	blocked, err := s.st.ListBlockedNodesForUser(userID)
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
// 以属主生效设置为基准（standardize_names / name_template,租户级回退全局默认），
// 再按订阅地址覆盖:ep.NameMode "on"/"off" 强制开关,ep.NameTemplate 非空则替换模板。
// ep 为 nil 时只取属主设置（用于无端点上下文的管理列表）。读设置失败时降级为不标准化。
// userID 为属主(多租户):0 = 全局视角(只读全局默认)。
func (s *Server) resolveNameConfig(userID int64, ep *store.Endpoint) (standardize bool, template string) {
	template = subscription.DefaultNameTemplate
	val, err := s.st.GetSettingForUser(userID, "standardize_names")
	if err != nil {
		s.logger.Warn("get settings failed, skipping name standardization", "error", err)
		return false, template
	}
	standardize = val == "true"
	if t, err := s.st.GetSettingForUser(userID, "name_template"); err == nil && t != "" {
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
// userID 为属主(多租户):管理列表(ep=nil)传调用方 effUID;订阅路径传端点属主。
func (s *Server) standardizeNodesForEndpoint(nodes []*subscription.Node, ep *store.Endpoint, userID int64) []*subscription.Node {
	std, tmpl := s.resolveNameConfig(userID, ep)
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
// renderSubscriptionForEndpoint generates subscription content with endpoint-aware template resolution.
// Four-level fallback chain for Clash templates (ticket endpoint-template-02):
// 1. endpoint.template_name (if set and exists in user's library)
// 2. user default template (is_default=1 in user's library)
// 3. system_settings.clash_template (global default set by super admin)
// 4. embedded default template (generator.DefaultTemplate)
//
// V2Ray format is unaffected (no template, just base64 link list).
// Soft reference: missing template at any level falls through to next level; never errors.
func (s *Server) renderSubscriptionForEndpoint(nodes []*subscription.Node, format string, ep *store.Endpoint) (data []byte, contentType string, err error) {
	switch format {
	case "v2ray":
		data, err = generator.GenerateV2Ray(nodes)
		return data, "text/plain; charset=utf-8", err
	default:
		// Four-level fallback chain
		tmpl, tErr := s.resolveTemplateForEndpoint(ep)
		if tErr != nil {
			return nil, "", fmt.Errorf("resolve template: %w", tErr)
		}
		data, err = generator.RenderTemplate(tmpl, nodes)
		return data, "text/yaml; charset=utf-8", err
	}
}

// resolveTemplateForEndpoint implements the 4-level fallback chain for template resolution.
// Returns template content (never empty), never errors (falls back to embedded default).
func (s *Server) resolveTemplateForEndpoint(ep *store.Endpoint) (string, error) {
	// Level 1: endpoint.template_name (soft reference; miss or empty falls through)
	if ep.TemplateName != "" {
		tmpl, err := s.st.GetTemplateByName(ep.UserID, ep.TemplateName)
		if err == nil && tmpl.Content != "" {
			return tmpl.Content, nil
		}
	}

	// Levels 2-4: user default ?? global default ?? embedded default
	// (same chain as the tenant-level template setting).
	return s.st.GetClashTemplateForUser(ep.UserID)
}

// handleEndpointPreview 后台预览：对指定订阅地址在“当前那一刻”生成订阅内容与节点清单，
// 应用与 /sub 完全相同的关键词过滤（所见即所得），但不记录拉取统计（见 ADR 0005）。
// ticket 07: 校验属主,行属他人 404。
func (s *Server) handleEndpointPreview(w http.ResponseWriter, r *http.Request) {
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

	format := r.URL.Query().Get("format")
	if format != "v2ray" {
		format = "clash"
	}

	// 与 /sub 同源的会下发节点集合(过滤链 + 条件 + 标准化),四处共用,见 endpoint_test.go
	nodes := s.endpointDeliverableNodes(ep)

	// 过滤后可能为空（节点池未就绪或全被过滤链剔除）；此时返回空内容而非 500，方便后台排查
	content := ""
	if len(nodes) > 0 {
		data, _, genErr := s.renderSubscriptionForEndpoint(nodes, format, ep)
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
