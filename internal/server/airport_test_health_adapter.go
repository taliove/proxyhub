package server

import (
	"context"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/healthcheck"
	"github.com/taliove/proxyhub/internal/subscription"
)

// HealthCheckAdapter adapts healthcheck.Checker to airporttest.HealthChecker
type HealthCheckAdapter struct {
	checker *healthcheck.Checker
}

func NewHealthCheckAdapter(checker *healthcheck.Checker) *HealthCheckAdapter {
	return &HealthCheckAdapter{checker: checker}
}

func (a *HealthCheckAdapter) CheckAll(ctx context.Context, nodes []*subscription.Node) []*airporttest.HealthCheckResult {
	results := a.checker.CheckAll(ctx, nodes)
	adapted := make([]*airporttest.HealthCheckResult, len(results))
	for i, r := range results {
		adapted[i] = &airporttest.HealthCheckResult{
			Node:      r.Node,
			Available: r.Available,
			Latency:   r.Latency,
			Error:     r.Error,
		}
	}
	return adapted
}
