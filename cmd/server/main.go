package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/alert"
	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/server"
	"github.com/taliove/proxyhub/internal/store"
)

func main() {
	// 子命令分发: 首个参数为已知子命令时走子命令路径,否则按服务启动处理
	if len(os.Args) > 1 && os.Args[1] == "state-fingerprint" {
		if err := runStateFingerprint(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
		return
	}

	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	logger := newLogger(cfg.Log)

	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 系统是否已初始化（首次运行需通过 Web 向导设置账号）
	initialized, err := st.IsSystemInitialized()
	if err != nil {
		return fmt.Errorf("检查初始化状态: %w", err)
	}

	// 首次启动播种默认解锁检测目标（仅当 detection_targets 不存在,不覆盖用户配置）
	if err := st.SeedDetectionTargets(); err != nil {
		return fmt.Errorf("播种检测目标: %w", err)
	}

	// 告警器从数据库设置动态读取 webhook（无需重启即可修改）
	alerter := alert.NewAlerter(st)

	// 聚合调度器（后台定时：拉取 → 检查 → 过滤 → 告警）
	agg := aggregator.New(cfg, alerter, st, logger)
	go agg.Run(ctx)

	// IP 地理位置解析（后台异步）
	resolver := geoip.NewResolver(st, "")
	go resolver.Run(ctx, 5*time.Minute)

	// 定期清理：审计日志保留 90 天、健康历史保留 30 天，避免数据库无限增长
	go runMaintenance(ctx, st, logger)

	// 初始化检测服务
	detectionSvc := initDetectionService(cfg, st, agg, logger)

	// HTTP 服务（SPA + API + 订阅端点）
	srv := server.New(cfg, st, agg, WebFS, logger, detectionSvc, resolver)
	srv.RecoverJobs()            // 重启恢复:遗留 running 体检任务标记 interrupted
	go srv.StartExamSweeper(ctx) // 后台清扫过期(超过 TTL)的体检任务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Printf("\n========================================\n")
	fmt.Printf("  ProxyHub 已启动\n")
	fmt.Printf("  监听地址:   http://%s\n", addr)
	if !initialized {
		fmt.Printf("  ⚠ 系统尚未初始化，请打开上述地址完成账号设置\n")
	}
	fmt.Printf("========================================\n\n")

	logger.Info("server starting", "addr", addr, "initialized", initialized)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 服务: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

// 数据保留策略（见 ADR 0010）
const (
	auditRetentionDays  = 90 // 审计日志保留天数
	healthRetentionDays = 30 // 健康检查历史保留天数
	maintenanceInterval = 24 * time.Hour
)

// runMaintenance 定期清理过期数据（审计日志、健康历史）。
// 启动时立即跑一次，之后每 maintenanceInterval 一次，直到上下文取消。
func runMaintenance(ctx context.Context, st *store.Store, logger *slog.Logger) {
	prune := func() {
		if err := st.PruneAuditLogs(time.Now().AddDate(0, 0, -auditRetentionDays)); err != nil {
			logger.Warn("prune audit logs failed", "error", err)
		}
		if err := st.PruneHealth(healthRetentionDays); err != nil {
			logger.Warn("prune health failed", "error", err)
		}
	}
	prune()

	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}

// newLogger 根据配置创建日志器
func newLogger(cfg config.LogConfig) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// initDetectionService 初始化节点解锁检测服务
func initDetectionService(cfg *config.Config, st *store.Store, nodes server.NodeSource, logger *slog.Logger) *server.DetectionService {
	// 导入 detection 包
	// 创建 detector 实例
	detector := detection.NewDetector(
		20,             // 节点并发数
		5*time.Second,  // TCP 快筛超时
		12*time.Second, // 真实代理请求超时
	)
	// 注入带宽配置提供函数（每次带宽测试实时从 settings 读取，缺省用默认值）
	detector.SetBandwidthConfigProvider(st.GetBandwidthConfig)
	// 注入体检配置提供函数（每场深度体检实时从 settings 读取，缺省用默认值）
	detector.SetExamConfigProvider(st.GetExamConfig)

	// 创建检测服务
	return server.NewDetectionService(
		detector,
		st,
		logger,
		nodes.Nodes,            // 获取节点池的函数
		st.GetDetectionTargets, // 获取检测目标的函数
	)
}
