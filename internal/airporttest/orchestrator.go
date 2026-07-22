package airporttest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
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

// RunTest 执行完整测试流水线:诊断(已完成)→ [条件:池空且URL通则upsert] → 抽样 → 检活写回 → 评分。
//
// 新流水线(pool-first):
//  1. 诊断已由 RunDiagnostic/handler 完成(diagResult 含 HTTP 状态)
//  2. 分支判断(需 poolOps 支持):
//     A. 池有该机场节点:直接用池节点测试,诊断结果仅作信息(URL不通不阻断)
//     B. 池无该机场节点:
//        - URL通(diagResult.HTTPStatus 2xx):upsert 拉到的节点入池,再测试
//        - URL不通:failed,error_message 明确("订阅URL不可达且池内无已同步节点")
//  3. 评分:URL不通时拉取健康N/A,权重重归一(可用率5/9+延迟3/9+地区1/9)
//
// airportName 用于 poolOps.LoadPoolBySource 匹配池内节点(按 nodes.source 字段)。
// 返回更新后的 run(含 overall_score/dimensions_json)。
func (o *Orchestrator) RunTest(ctx context.Context, run *TestRun, airportName string, fetchedNodes []*subscription.Node, diagResult *DiagnosticResult) (*TestRun, error) {
	urlReachable := diagResult.HTTPStatus >= 200 && diagResult.HTTPStatus < 300

	// 决定测试哪批节点:优先用池,池空时条件入池
	var nodesToTest []*subscription.Node
	poolHasNodes := false

	if o.poolOps != nil {
		// 查询池中该机场节点
		poolNodes, err := o.poolOps.LoadPoolBySource(airportName)
		if err != nil {
			return nil, fmt.Errorf("load pool by source: %w", err)
		}
		poolHasNodes = len(poolNodes) > 0

		if poolHasNodes {
			// 分支A:池有节点,直接测试(URL不通不阻断)
			nodesToTest = poolNodes
		} else {
			// 分支B:池无节点
			if !urlReachable || len(fetchedNodes) == 0 {
				// URL不通或拉取失败:run failed
				run.Status = StatusFailed
				run.ErrorMessage = "subscription URL unreachable and no synced nodes in pool"
				if err := o.store.UpdateTestRun(ctx, run); err != nil {
					return nil, fmt.Errorf("update failed run: %w", err)
				}
				return run, nil
			}
			// URL通且有节点:upsert入池
			if err := o.poolOps.UpsertAirportNodes(airportName, fetchedNodes); err != nil {
				return nil, fmt.Errorf("upsert airport nodes: %w", err)
			}
			nodesToTest = fetchedNodes
		}
	} else {
		// 无poolOps(兼容旧测试):用传入节点
		nodesToTest = fetchedNodes
	}

	// 节点为空:完成,零分(仅在池空+URL不通已fail时不会走到这)
	if len(nodesToTest) == 0 {
		score, dims := CalculateScore(nodesToTest, diagResult.HTTPStatus, diagResult.ParseFailures, diagResult.NodeCount+diagResult.ParseFailures)
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

	sampled := SampleNodes(nodesToTest, run.IsFull)
	sampleParams := map[string]interface{}{
		"full":         run.IsFull,
		"total":        len(nodesToTest),
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
			// 写回节点池(复用 aggregator.UpdateNodeTestResult);失败时分类记录原因(ticket 0017)
			failReason, failDetail := "", ""
			if !r.Available && r.Error != nil {
				failReason = detection.ClassifyFailure(r.Error)
				failDetail = r.Error.Error()
			}
			o.poolWriter.UpdateNodeTestResult(r.Node.NodeKey(), "quick", r.Available, r.Latency, 0, 0, failReason, failDetail)
			// 同时更新内存中的节点状态,供评分使用
			r.Node.Available = r.Available
			r.Node.Latency = r.Latency
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

	// 使用全部测试节点评分(不仅是样本,反映整体质量)
	// CalculateScore 内部按 httpStatus 自动重归一权重
	score, dims := CalculateScore(nodesToTest, diagResult.HTTPStatus, diagResult.ParseFailures, diagResult.NodeCount+diagResult.ParseFailures)
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
