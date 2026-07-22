package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// 实测 run 状态(与机场测试 run 状态字符串对齐)。
const (
	probeRunRunning   = "running"
	probeRunCompleted = "completed"
	probeRunFailed    = "failed"
)

// endpointProbeRunTTL 内存态 run 的过期时间:进度更新刷新基准,
// 完成/失败后保留一个 TTL 供前端轮询,随后清理。
const endpointProbeRunTTL = 30 * time.Minute

// endpointProbeRun 订阅现场实测的内存态 run(ADR 0028 决策 5:不落库,重启即弃)。
// 实例归属 probeRunRegistry,字段只在注册表锁内变更;外部一律持 snapshot 副本。
type endpointProbeRun struct {
	RunID      string `json:"run_id"`
	EndpointID int64  `json:"endpoint_id"`
	Full       bool   `json:"full"`
	Status     string `json:"status"` // running/completed/failed
	Total      int    `json:"total"`  // 会下发节点数
	Sampled    int    `json:"sampled"`
	Checked    int    `json:"checked"`
	Error      string `json:"error,omitempty"`

	createdAt time.Time
	updatedAt time.Time
}

// snapshot 返回脱离锁可安全读取的副本。
func (r *endpointProbeRun) snapshot() endpointProbeRun { return *r }

// probeRunRegistry 内存态 run 注册表:map + mutex + TTL 惰性清理(无后台 goroutine)。
// 实测 goroutine 写进度与轮询读并发共享,所有访问必须过锁。
type probeRunRegistry struct {
	mu   sync.Mutex
	runs map[string]*endpointProbeRun
	ttl  time.Duration
	now  func() time.Time // 可注入,测试用
}

func newProbeRunRegistry(ttl time.Duration) *probeRunRegistry {
	return &probeRunRegistry{
		runs: make(map[string]*endpointProbeRun),
		ttl:  ttl,
		now:  time.Now,
	}
}

// create 登记一个新 run(running 态),返回其快照。
func (r *probeRunRegistry) create(endpointID int64, full bool, total int) endpointProbeRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked(r.now())
	run := &endpointProbeRun{
		RunID:      newProbeRunID(),
		EndpointID: endpointID,
		Full:       full,
		Status:     probeRunRunning,
		Total:      total,
		createdAt:  r.now(),
		updatedAt:  r.now(),
	}
	r.runs[run.RunID] = run
	return run.snapshot()
}

// get 按 id 取 run 快照;不存在或已过期返回 ok=false(调用方映射 404)。
func (r *probeRunRegistry) get(id string) (endpointProbeRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked(r.now())
	run, ok := r.runs[id]
	if !ok {
		return endpointProbeRun{}, false
	}
	return run.snapshot(), true
}

func (r *probeRunRegistry) markSampled(id string, sampled int) {
	r.update(id, func(run *endpointProbeRun) { run.Sampled = sampled })
}

func (r *probeRunRegistry) markChecked(id string, checked int) {
	r.update(id, func(run *endpointProbeRun) { run.Checked = checked })
}

// finish 收口 run:err 为 nil 置 completed,否则置 failed 并记录原因。
func (r *probeRunRegistry) finish(id string, err error) {
	r.update(id, func(run *endpointProbeRun) {
		if err != nil {
			run.Status = probeRunFailed
			run.Error = err.Error()
		} else {
			run.Status = probeRunCompleted
		}
	})
}

// update 在锁内变更 run 并刷新过期基准;run 不存在(已过期/重启)时静默丢弃。
func (r *probeRunRegistry) update(id string, mutate func(*endpointProbeRun)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return
	}
	mutate(run)
	run.updatedAt = r.now()
}

// sweepLocked 清理超龄 run(以最近更新时间为基准)。调用方必须已持锁。
func (r *probeRunRegistry) sweepLocked(now time.Time) {
	for id, run := range r.runs {
		if now.Sub(run.updatedAt) > r.ttl {
			delete(r.runs, id)
		}
	}
}

// newProbeRunID 生成随机 run id(128bit hex)。crypto/rand 失败时退化为纳秒时间戳 id,
// 实践中不会触发,仅避免 panic。
func newProbeRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
