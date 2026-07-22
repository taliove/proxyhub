package airporttest

import (
	"context"
	"errors"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// ProbeOutcome reports the per-node result of a probe pass.
type ProbeOutcome struct {
	Node      *subscription.Node // sampled node; mutated in place with check results when Checked
	Checked   bool               // false when no checker/writer was configured
	Available bool
	Latency   int
	Error     error
}

// ProbeHooks let callers observe sampling progress without the core
// persisting anything itself. OnSampled fires once after sampling and
// before any check; returning an error aborts the probe. OnProgress
// fires after each node's writeback with the 1-based checked count.
type ProbeHooks struct {
	OnSampled  func(sampled int) error
	OnProgress func(checked int)
}

// ProbeCore is the persistence-free sampling + health-check + writeback
// core shared by airport tests and endpoint (subscription) tests
// (ADR 0028, decision 3). It knows nothing about AirportID, test-run
// persistence, scoring, or HTTP diagnostics.
type ProbeCore struct {
	checker HealthChecker
	writer  PoolWriter
}

// NewProbeCore creates a probe core from a health checker and a pool writer.
// Either may be nil; probing then only samples and reports outcomes.
func NewProbeCore(checker HealthChecker, writer PoolWriter) *ProbeCore {
	return &ProbeCore{checker: checker, writer: writer}
}

// Probe runs region-stratified sampling over nodes (full=true checks all),
// health-checks the sample, writes availability back through the pool
// writer, and returns one outcome per checked node. Checked nodes are
// mutated in place (Available/Latency) so callers can score the original
// node set. Run persistence is the caller's concern, driven via hooks.
// Cancellation: cancel-induced failures (ctx.Canceled once ctx is done) are
// not real measurements — they are neither written back nor counted in
// progress, and their outcomes are reported with Checked=false (issue 0025).
func (c *ProbeCore) Probe(ctx context.Context, nodes []*subscription.Node, full bool, hooks ProbeHooks) ([]*ProbeOutcome, error) {
	sampled := SampleNodes(nodes, full)
	if hooks.OnSampled != nil {
		if err := hooks.OnSampled(len(sampled)); err != nil {
			return nil, err
		}
	}

	if c.checker == nil || c.writer == nil {
		return uncheckedOutcomes(sampled), nil
	}

	results := c.checker.CheckAll(ctx, sampled)
	cancelled := ctx.Err() != nil
	outcomes := make([]*ProbeOutcome, 0, len(results))
	checked := 0
	for _, r := range results {
		// 取消诱导的失败(ctx.Canceled)不是真实测量:不回写池、不计进度,
		// 避免把"被取消"误诊为节点不可用污染池状态(issue 0025)。
		// outcome 仍记录(Checked=false),调用方据此排除明细/统计。
		if cancelled && errors.Is(r.Error, context.Canceled) {
			outcomes = append(outcomes, &ProbeOutcome{Node: r.Node, Error: r.Error})
			continue
		}
		c.writeBack(r)
		checked++
		outcomes = append(outcomes, &ProbeOutcome{
			Node:      r.Node,
			Checked:   true,
			Available: r.Available,
			Latency:   r.Latency,
			Error:     r.Error,
		})
		if hooks.OnProgress != nil {
			hooks.OnProgress(checked)
		}
	}
	return outcomes, nil
}

// uncheckedOutcomes builds outcomes for a sample that was not health-checked.
func uncheckedOutcomes(sampled []*subscription.Node) []*ProbeOutcome {
	outcomes := make([]*ProbeOutcome, 0, len(sampled))
	for _, n := range sampled {
		outcomes = append(outcomes, &ProbeOutcome{Node: n})
	}
	return outcomes
}

// writeBack persists one check result to the node pool (reusing the global
// health-check writeback path) and mirrors it onto the in-memory node.
// Failures are classified by reason (ticket 0017).
func (c *ProbeCore) writeBack(r *HealthCheckResult) {
	failReason, failDetail := "", ""
	if !r.Available && r.Error != nil {
		failReason = detection.ClassifyFailure(r.Error)
		failDetail = r.Error.Error()
	}
	c.writer.UpdateNodeTestResult(r.Node.NodeKey(), "quick", r.Available, r.Latency, 0, 0, failReason, failDetail)
	r.Node.Available = r.Available
	r.Node.Latency = r.Latency
}
