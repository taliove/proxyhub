package airporttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 机场测试任务的 kind 名与 key 编码(issue 0025:迁入 jobs 运行时,ADR 0019 收口)。
const JobKindName = "airport_test"

// JobKey 任务 key:与单机场刷新同编码("airport-<id>"),跨 kind 互斥据此对齐机场。
func JobKey(airportID int64) string {
	return fmt.Sprintf("airport-%d", airportID)
}

// JobParams 机场测试任务启动参数(params_json)。
type JobParams struct {
	AirportID   int64  `json:"airport_id"`
	AirportName string `json:"airport_name"`
	AirportURL  string `json:"airport_url"`
	Full        bool   `json:"full"`
}

// jobCursor 进度游标(jobs.cursor 持久化,任务中心/前端轮询 /api/jobs/{id} 消费)。
// 主进度源;run 行 sample_params 为镜像(决策:cursor 为主,run 列照旧写)。
type jobCursor struct {
	Phase   string `json:"phase"`   // diagnosing / checking / scoring
	Checked int    `json:"checked"` // checking 阶段已检活样本数
	Total   int    `json:"total"`   // checking 阶段样本总数
}

// FetchFunc 订阅拉取缝(测试注入)。池感知语义:拉取失败不视为错误,
// 返回 HTTPStatus=0 的诊断与 nil 节点,由编排层按池分支裁决(池空+URL不通才 failed)。
type FetchFunc func(ctx context.Context, name, url string) (*DiagnosticResult, []*subscription.Node)

// SubscriptionFetch 生产拉取实现:复用 subscription.Fetcher(ctx 中断即取消拉取),
// 诊断口径与 RunDiagnostic 同源(ParseWithStats 统计)。
func SubscriptionFetch(f *subscription.Fetcher) FetchFunc {
	return func(ctx context.Context, name, url string) (*DiagnosticResult, []*subscription.Node) {
		sub, diag, err := f.FetchContext(ctx, name, url)
		result := &DiagnosticResult{
			HTTPStatus:     diag.HTTPStatus,
			DurationMs:     diag.DurationMs,
			NodeCount:      diag.NodeCount,
			ParseFailures:  diag.ParseFailures,
			ProtocolCounts: make(map[string]int),
		}
		if err != nil {
			return result, nil
		}
		for _, n := range sub.Nodes {
			result.ProtocolCounts[n.Type]++
		}
		return result, sub.Nodes
	}
}

// JobKind 机场测试作为 jobs 运行时的 kind(包装 Orchestrator)。
// 不可续跑(Resumable=false):重启即 interrupted,对齐 refresh;
// 取消=ctx 中断,已写回的抽样检活结果不回滚,run 行标 cancelled。
type JobKind struct {
	orch    *Orchestrator
	store   Store
	fetch   FetchFunc
	jobIDOf func(key string) int64
}

// NewJobKind 构造 kind。jobIDOf 为写入侧反查 jobs 行 id(ADR 0026 样板:
// Insert 先于 Run 保证查到,同 kind+key 单实例保证唯一,查不到退化 0)。
func NewJobKind(orch *Orchestrator, st Store, fetch FetchFunc, jobIDOf func(key string) int64) *JobKind {
	return &JobKind{orch: orch, store: st, fetch: fetch, jobIDOf: jobIDOf}
}

func (k *JobKind) Name() string    { return JobKindName }
func (k *JobKind) Resumable() bool { return false }

// Run 执行一次机场测试:诊断拉取 → 建行(带诊断数据 + job_id)→ 编排(抽样/检活/评分)。
// 返回 ctx.Err()=被取消(jobs 行 cancelled);run failed 映射为错误(jobs 行 failed);
// 自然完成返回 nil(jobs 行 done)。
func (k *JobKind) Run(ctx context.Context, params json.RawMessage, _ string, _ func(json.RawMessage), progress func(string)) error {
	var p JobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("parse airport test params: %w", err)
	}

	reportProgress(progress, jobCursor{Phase: string(StatusDiagnosing)})

	diag, nodes := k.fetch(ctx, p.AirportName, p.AirportURL)
	// 拉取期间被取消:尚未建行,直接以取消收口(jobs 行 cancelled,无 run 产出)
	if err := ctx.Err(); err != nil {
		return err
	}

	// 建行:诊断数据 + job_id 关联。持久化走脱离取消的 ctx(取消后收口仍须落库)。
	run := &TestRun{
		AirportID:    p.AirportID,
		CreatedAt:    time.Now().UTC(),
		SampleParams: "{}",
		IsFull:       p.Full,
		Status:       StatusDiagnosing,
		JobID:        k.jobIDOf(JobKey(p.AirportID)),
	}
	dimsJSON, _ := json.Marshal(diag)
	run.DimensionsJSON = string(dimsJSON)
	runID, err := k.store.CreateTestRun(context.WithoutCancel(ctx), run)
	if err != nil {
		return fmt.Errorf("create test run: %w", err)
	}
	run.ID = runID

	run, err = k.orch.RunTest(ctx, run, p.AirportName, nodes, diag, func(phase string, checked, total int) {
		reportProgress(progress, jobCursor{Phase: phase, Checked: checked, Total: total})
	})
	if err != nil {
		// ctx 取消:run 行已被 RunTest 标 cancelled,jobs 行由运行时标 cancelled
		if errors.Is(err, context.Canceled) {
			return err
		}
		return err
	}
	// 编排内业务失败(池空+URL不通):run 行 failed,jobs 行同步 failed(任务中心口径一致)
	if run.Status == StatusFailed {
		return fmt.Errorf("airport test failed: %s", run.ErrorMessage)
	}
	return nil
}

// reportProgress 上报进度游标(jobs 运行时立即持久化 cursor)。
func reportProgress(progress func(string), c jobCursor) {
	if progress == nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	progress(string(data))
}
