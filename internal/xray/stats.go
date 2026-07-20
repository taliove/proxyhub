package xray

import (
	"fmt"
)

// StatsClient provides access to Xray stats API
type StatsClient struct {
	apiAddr string
}

// PathStats represents traffic statistics for a distribution path
type PathStats struct {
	Upload      int64 // Bytes uploaded
	Download    int64 // Bytes downloaded
	Connections int64 // Active connections
}

// NewStatsClient creates a new stats client
func NewStatsClient(apiAddr string) *StatsClient {
	if apiAddr == "" {
		apiAddr = "127.0.0.1:10085"
	}
	return &StatsClient{
		apiAddr: apiAddr,
	}
}

// GetPathStats retrieves traffic statistics for a specific path
// TODO: implement real gRPC client using Xray stats service proto
// For now, returns mock data for testing
func (sc *StatsClient) GetPathStats(path string) (*PathStats, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Mock implementation - replace with real gRPC call to Xray stats API
	// Real implementation should:
	// 1. Connect to gRPC server at sc.apiAddr
	// 2. Call StatsService.QueryStats with pattern matching the outbound tag
	// 3. Parse uplink/downlink counters
	// 4. Return aggregated PathStats

	stats := &PathStats{
		Upload:      1024 * 1024 * 100, // 100 MB
		Download:    1024 * 1024 * 500, // 500 MB
		Connections: 42,
	}

	return stats, nil
}

// GetAllStats retrieves statistics for all paths
// TODO: implement real gRPC client
func (sc *StatsClient) GetAllStats() (map[string]*PathStats, error) {
	// Mock implementation
	return map[string]*PathStats{
		"path1": {
			Upload:      1024 * 1024 * 100,
			Download:    1024 * 1024 * 500,
			Connections: 42,
		},
		"path2": {
			Upload:      1024 * 1024 * 200,
			Download:    1024 * 1024 * 800,
			Connections: 38,
		},
	}, nil
}

// ResetPathStats resets statistics counters for a path
// TODO: implement real gRPC client
func (sc *StatsClient) ResetPathStats(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}

	// Mock implementation - real implementation should call Xray API
	return nil
}
