package airporttest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

const subscriptionUserAgent = "v2rayN/6.23"

// RunDiagnostic executes diagnostic phase for an airport.
// Returns the created run with diagnostic results. Does not modify node pool.
func (o *Orchestrator) RunDiagnostic(ctx context.Context, airportID int64, airportName, airportURL string, isFull bool) (*TestRun, error) {
	run := &TestRun{
		AirportID:    airportID,
		CreatedAt:    time.Now().UTC(),
		SampleParams: "{}",
		IsFull:       isFull,
		Status:       StatusDiagnosing,
	}

	start := time.Now()

	// Fetch raw subscription
	req, err := http.NewRequest(http.MethodGet, airportURL, nil)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("fetch failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("read body: %w", err))
	}

	// Decode base64 if needed
	decoded, err := base64.RawStdEncoding.DecodeString(string(body))
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			decoded = body
		}
	}

	// Parse with stats
	parseResult := subscription.ParseWithStats(string(decoded), airportName)

	if len(parseResult.Nodes) == 0 && parseResult.ParseFailures == parseResult.TotalLines {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("no valid nodes found"))
	}

	// Count protocols
	protocolCounts := make(map[string]int)
	for _, node := range parseResult.Nodes {
		protocolCounts[node.Type]++
	}

	diag := DiagnosticResult{
		HTTPStatus:     resp.StatusCode,
		DurationMs:     elapsed.Milliseconds(),
		NodeCount:      len(parseResult.Nodes),
		ProtocolCounts: protocolCounts,
		ParseFailures:  parseResult.ParseFailures,
	}

	dimJSON, _ := json.Marshal(diag)
	run.Status = StatusCompleted
	run.DimensionsJSON = string(dimJSON)

	id, err := o.store.CreateTestRun(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("persist run: %w", err)
	}
	run.ID = id
	return run, nil
}

func (o *Orchestrator) persistFailedRun(ctx context.Context, run *TestRun, start time.Time, err error) (*TestRun, error) {
	run.Status = StatusFailed
	run.ErrorMessage = err.Error()
	dimJSON, _ := json.Marshal(DiagnosticResult{
		HTTPStatus: 0,
		DurationMs: time.Since(start).Milliseconds(),
	})
	run.DimensionsJSON = string(dimJSON)
	id, dbErr := o.store.CreateTestRun(ctx, run)
	if dbErr != nil {
		return nil, fmt.Errorf("persist failed run: %w", dbErr)
	}
	run.ID = id
	return run, nil
}

// RunTest 执行完整测试流水线:诊断(已完成)→ 抽样 → 检活写回 → 评分。
// 诊断已由 RunDiagnostic 完成,传入其返回的节点列表。
// 返回更新后的 run(含 overall_score/dimensions_json)。
func (o *Orchestrator) RunTest(ctx context.Context, run *TestRun, nodes []*subscription.Node, diagResult *DiagnosticResult) (*TestRun, error) {
	// 节点池为空:直接完成,可用率维度为0
	if len(nodes) == 0 {
		score, dims := CalculateScore(nodes, diagResult.HTTPStatus, diagResult.ParseFailures, diagResult.NodeCount+diagResult.ParseFailures)
		dimsJSON, _ := json.Marshal(dims)
		scoreVal := score
		run.Status = StatusCompleted
		run.OverallScore = &scoreVal
		run.DimensionsJSON = string(dimsJSON)
		if err := o.store.UpdateTestRun(ctx, run); err != nil {
			return nil, fmt.Errorf("update run: %w", err)
		}
		return run, nil
	}

	// 阶段1:抽样
	run.Status = StatusChecking
	if err := o.store.UpdateTestRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update to checking: %w", err)
	}

	sampled := SampleNodes(nodes, run.IsFull)
	sampleParams := map[string]interface{}{
		"full":         run.IsFull,
		"total":        len(nodes),
		"sampled":      len(sampled),
		"checked":      0,
		"total_sample": len(sampled),
	}
	paramsJSON, _ := json.Marshal(sampleParams)
	run.SampleParams = string(paramsJSON)
	if err := o.store.UpdateTestRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update sample params: %w", err)
	}

	// 阶段2:检活写回(复用全局健康检查路径)
	if o.healthChecker != nil && o.poolWriter != nil {
		results := o.healthChecker.CheckAll(ctx, sampled)
		for i, r := range results {
			// 写回节点池(复用 aggregator.UpdateNodeTestResult)
			o.poolWriter.UpdateNodeTestResult(r.Node.NodeKey(), "quick", r.Available, r.Latency, 0, 0)
			// 更新进度
			sampleParams["checked"] = i + 1
			paramsJSON, _ = json.Marshal(sampleParams)
			run.SampleParams = string(paramsJSON)
			o.store.UpdateTestRun(ctx, run)
		}
	}

	// 阶段3:评分
	run.Status = StatusScoring
	if err := o.store.UpdateTestRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update to scoring: %w", err)
	}

	// 使用全池节点评分(不仅是样本,反映整体质量)
	score, dims := CalculateScore(nodes, diagResult.HTTPStatus, diagResult.ParseFailures, diagResult.NodeCount+diagResult.ParseFailures)
	dimsJSON, _ := json.Marshal(dims)
	scoreVal := score
	run.Status = StatusCompleted
	run.OverallScore = &scoreVal
	run.DimensionsJSON = string(dimsJSON)
	if err := o.store.UpdateTestRun(ctx, run); err != nil {
		return nil, fmt.Errorf("finalize run: %w", err)
	}
	return run, nil
}
