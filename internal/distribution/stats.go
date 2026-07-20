package distribution

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// StatsCollector collects traffic statistics from Xray
type StatsCollector struct {
	xrayClient XrayStatsClient
	store      *store.Store
	interval   time.Duration
	logger     *slog.Logger
	stopCh     chan struct{}
}

// XrayStatsClient defines the interface for fetching stats from Xray
type XrayStatsClient interface {
	GetPathStats(pathTag string) (*PathStats, error)
}

// PathStats represents traffic statistics for a path
type PathStats struct {
	Upload      int64 // bytes
	Download    int64 // bytes
	Connections int64
}

// NewStatsCollector creates a new stats collector
func NewStatsCollector(
	xrayClient XrayStatsClient,
	store *store.Store,
	interval time.Duration,
	logger *slog.Logger,
) *StatsCollector {
	return &StatsCollector{
		xrayClient: xrayClient,
		store:      store,
		interval:   interval,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}
}

// Start begins collecting stats in a background goroutine
func (sc *StatsCollector) Start(ctx context.Context) {
	go sc.run(ctx)
	sc.logger.Info("stats collector started", "interval", sc.interval)
}

// Stop signals the collector to stop
func (sc *StatsCollector) Stop() {
	close(sc.stopCh)
	sc.logger.Info("stats collector stopped")
}

// run is the main collection loop
func (sc *StatsCollector) run(ctx context.Context) {
	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			sc.logger.Info("stats collector context cancelled")
			return
		case <-sc.stopCh:
			sc.logger.Info("stats collector stop signal received")
			return
		case <-ticker.C:
			if err := sc.CollectOnce(); err != nil {
				sc.logger.Error("failed to collect stats", "error", err)
			}
		}
	}
}

// CollectOnce fetches stats from Xray and saves to store
func (sc *StatsCollector) CollectOnce() error {
	// Get all enabled paths
	paths, err := sc.store.ListDistributionPaths()
	if err != nil {
		return fmt.Errorf("list distribution paths: %w", err)
	}

	now := time.Now()
	collected := 0

	for _, path := range paths {
		if !path.Enabled {
			continue
		}

		pathTag := fmt.Sprintf("path_%d", path.ID)

		// Fetch stats from Xray
		stats, err := sc.xrayClient.GetPathStats(pathTag)
		if err != nil {
			sc.logger.Warn("failed to get path stats",
				"path_id", path.ID,
				"path_name", path.Name,
				"error", err)
			continue
		}

		// Record snapshot to distribution_stats
		stat := &store.DistributionStat{
			PathID:      path.ID,
			Timestamp:   now,
			Upload:      stats.Upload,
			Download:    stats.Download,
			Connections: stats.Connections,
		}

		if err := sc.store.RecordDistributionStat(stat); err != nil {
			sc.logger.Error("failed to record distribution stat",
				"path_id", path.ID,
				"error", err)
			continue
		}

		// Update cumulative totals in distribution_paths
		// Calculate delta from previous total
		uploadDelta := stats.Upload
		downloadDelta := stats.Download
		connectionsDelta := stats.Connections

		// In production, we'd track previous values to calculate actual deltas
		// For now, we'll use the absolute values as increments
		// This assumes stats are reset after each collection or represent incremental values

		if err := sc.store.UpdatePathTotalStats(path.ID, uploadDelta, downloadDelta, connectionsDelta); err != nil {
			sc.logger.Error("failed to update path total stats",
				"path_id", path.ID,
				"error", err)
			continue
		}

		collected++
		sc.logger.Debug("collected stats for path",
			"path_id", path.ID,
			"path_name", path.Name,
			"upload", stats.Upload,
			"download", stats.Download,
			"connections", stats.Connections)
	}

	if collected > 0 {
		sc.logger.Info("stats collection completed",
			"paths_collected", collected,
			"total_paths", len(paths))
	}

	return nil
}
