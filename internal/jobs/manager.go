package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	// defaultBufferCap 每个任务事件环形缓冲上限:附加时回放最近这么多帧,溢出丢最旧。
	defaultBufferCap = 512
	// defaultTTL 任务结束后结果保留时长,供迟到附加回放,过期清理。
	defaultTTL = 5 * time.Minute
)

// jobID 任务身份:同一 kind 下按 key 单实例。
type jobID struct {
	kind string
	key  string
}

// job 单个运行中/已收口的任务:自带 context、环形事件缓冲、订阅者列表。
type job struct {
	id   int64 // jobs 表行 id(未持久化时为 0)
	kind Kind
	key  string

	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	buffer      []Event
	nextSeq     int
	subscribers map[int]chan Event
	nextSubID   int
	cancelled   bool
	done        bool
	finishedAt  time.Time
}

// pushLocked 分配序号、写入缓冲并非阻塞广播给订阅者(调用方持有 j.mu)。
// 通道有缓冲,正常读者不满;满则丢弃该帧(仍可由环形缓冲回放找回)。
func (j *job) pushLocked(data json.RawMessage, bufCap int) {
	frame := Event{Seq: j.nextSeq, Data: data}
	j.nextSeq++
	if len(j.buffer) >= bufCap {
		copy(j.buffer, j.buffer[1:])
		j.buffer = j.buffer[:len(j.buffer)-1]
	}
	j.buffer = append(j.buffer, frame)
	for _, ch := range j.subscribers {
		select {
		case ch <- frame:
		default:
		}
	}
}

// subscribe 附加订阅者:原子快照缓冲(replay)并注册直播通道(live)。
// 任务已收口时返回全量 replay 与一个已关闭通道,unsub 为空操作。
func (j *job) subscribe(bufCap int) (replay []Event, live <-chan Event, unsub func()) {
	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot := make([]Event, len(j.buffer))
	copy(snapshot, j.buffer)

	if j.done {
		ch := make(chan Event)
		close(ch)
		return snapshot, ch, func() {}
	}

	ch := make(chan Event, bufCap)
	id := j.nextSubID
	j.nextSubID++
	j.subscribers[id] = ch

	var once sync.Once
	return snapshot, ch, func() {
		once.Do(func() {
			j.mu.Lock()
			defer j.mu.Unlock()
			if c, ok := j.subscribers[id]; ok {
				delete(j.subscribers, id)
				close(c)
			}
		})
	}
}

// requestCancel 请求取消:已收口返回 false;否则置位并取消 ctx。
func (j *job) requestCancel() bool {
	j.mu.Lock()
	if j.done {
		j.mu.Unlock()
		return false
	}
	already := j.cancelled
	j.cancelled = true
	j.mu.Unlock()
	if !already {
		j.cancel()
	}
	return true
}

// finalize 收口任务(单锁原子):读取最终取消状态,取消则补发终止帧,
// 记录完成时刻、置 done,关闭在册订阅通道。返回是否取消(供落终态决策,消除竞态)。
func (j *job) finalize(now time.Time, cancelData json.RawMessage, hasCancel bool, bufCap int) (cancelled bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	cancelled = j.cancelled
	if cancelled && hasCancel {
		j.pushLocked(cancelData, bufCap)
	}
	j.done = true
	j.finishedAt = now
	for id, ch := range j.subscribers {
		close(ch)
		delete(j.subscribers, id)
	}
	return cancelled
}

// Subscription 一次附加:先回放 Replay(带序号),再从 Live 转直播。结束须 Close 退订。
type Subscription struct {
	Replay []Event
	Live   <-chan Event
	unsub  func()
}

// Close 退订本次附加(幂等;零值订阅为空操作)。
func (s *Subscription) Close() {
	if s.unsub != nil {
		s.unsub()
	}
}

// Manager 通用任务管理器:注册 kind、按 kind+key 单实例调度、事件缓冲/订阅/取消/TTL。
// store 非空时持久化任务生命周期(running/cursor/终态),支持重启续跑。
type Manager struct {
	store     *Store
	ttl       time.Duration
	bufferCap int
	now       func() time.Time
	onErr     func(error)

	mu    sync.Mutex
	kinds map[string]Kind
	jobs  map[jobID]*job
}

// Option 配置 Manager。
type Option func(*Manager)

// WithClock 注入时钟(TTL 虚拟时钟测试)。
func WithClock(now func() time.Time) Option { return func(m *Manager) { m.now = now } }

// WithTTL 设置已完成任务保留时长。
func WithTTL(d time.Duration) Option { return func(m *Manager) { m.ttl = d } }

// WithBufferCap 设置事件环形缓冲容量。
func WithBufferCap(n int) Option { return func(m *Manager) { m.bufferCap = n } }

// WithErrorHandler 注入持久化错误回调(默认丢弃;调用方应接到日志)。
func WithErrorHandler(h func(error)) Option { return func(m *Manager) { m.onErr = h } }

// NewManager 构造管理器。store 可为 nil(纯内存,不持久化)。
func NewManager(store *Store, opts ...Option) *Manager {
	m := &Manager{
		store:     store,
		ttl:       defaultTTL,
		bufferCap: defaultBufferCap,
		now:       time.Now,
		onErr:     func(error) {},
		kinds:     make(map[string]Kind),
		jobs:      make(map[jobID]*job),
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Register 注册一个任务 kind。重复注册直接 panic:这是编程错误,须在启动/测试时立刻暴露。
func (m *Manager) Register(k Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.kinds[k.Name()]; dup {
		panic(fmt.Sprintf("jobs: duplicate kind registration %q", k.Name()))
	}
	m.kinds[k.Name()] = k
}

// Open 启动或附加(kind,key)任务,返回一次订阅(回放 + 直播)。
func (m *Manager) Open(kind, key string, params json.RawMessage) (*Subscription, error) {
	return m.open(kind, key, params, false)
}

// OpenForce 强制重开:已收口的旧任务丢弃重开(避免 TTL 窗口内附加旧结果);
// 进行中的任务不打断,按 Open 语义附加。
func (m *Manager) OpenForce(kind, key string, params json.RawMessage) (*Subscription, error) {
	return m.open(kind, key, params, true)
}

// OpenIDForce 启动或附加(kind,key)任务:进行中的仍按附加,已收口的旧任务强制重开。
// 返回持久化行 ID 与是否本次新启动;rowID=0 表示持久化失败退化为纯内存任务。
// 适合"再点一次就再跑一轮"的触发语义(如刷新)。
func (m *Manager) OpenIDForce(kind, key string, params json.RawMessage) (rowID int64, started bool, err error) {
	return m.openID(kind, key, params, true)
}

func (m *Manager) openID(kind, key string, params json.RawMessage, force bool) (int64, bool, error) {
	j, created, err := m.startOrAttach(kind, key, params, force)
	if err != nil {
		return 0, false, err
	}
	return j.id, created, nil
}

func (m *Manager) open(kind, key string, params json.RawMessage, force bool) (*Subscription, error) {
	j, _, err := m.startOrAttach(kind, key, params, force)
	if err != nil {
		return nil, err
	}
	replay, live, unsub := j.subscribe(m.bufferCap)
	return &Subscription{Replay: replay, Live: live, unsub: unsub}, nil
}

// startOrAttach 无任务(或已过期)则启动新任务,否则返回现有任务。
// force 仅对已收口的旧任务生效:丢弃并重开。created 报告是否本次新启动。
func (m *Manager) startOrAttach(kind, key string, params json.RawMessage, force bool) (*job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, ok := m.kinds[kind]
	if !ok {
		return nil, false, fmt.Errorf("jobs: unknown kind %q", kind)
	}

	m.sweepLocked(m.now())

	id := jobID{kind: kind, key: key}
	if j, ok := m.jobs[id]; ok {
		if force && j.isDone() {
			delete(m.jobs, id)
		} else {
			return j, false, nil
		}
	}

	// 持久化启动为尽力而为:落库失败只记录,不阻塞任务运行(可用性优先于可续跑;
	// rowID=0 的任务不持久化生命周期,退化为纯内存任务)。
	var rowID int64
	if m.store != nil {
		newID, err := m.store.Insert(kind, key, params)
		if err != nil {
			m.onErr(fmt.Errorf("jobs: persist start: %w", err))
		} else {
			rowID = newID
		}
	}

	j := m.newJob(k, key, rowID)
	m.jobs[id] = j
	// OnStart 在锁内、goroutine 启动前同步调用:让 kind 把运行首步就要读的旁路数据
	// (体检活节点)原子就位,消除 Run 的 LoadAndDelete 抢先于旁路写入的竞态。
	if s, ok := k.(starter); ok {
		s.OnStart(key)
	}
	go m.runJob(j, params, "")
	return j, true, nil
}

func (m *Manager) newJob(k Kind, key string, rowID int64) *job {
	// 任务自带 context.Background() 派生的 ctx —— 绝不从请求 ctx 派生,连接断开不杀任务。
	ctx, cancel := context.WithCancel(context.Background())
	return &job{
		id:          rowID,
		kind:        k,
		key:         key,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[int]chan Event),
	}
}

func (j *job) isDone() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

// runJob 任务生命周期:跑 kind.Run -> 收口(取消则补终止帧)-> 落终态 -> 自然完成则通知 hook。
func (m *Manager) runJob(j *job, params json.RawMessage, cursor string) {
	emit := func(data json.RawMessage) {
		j.mu.Lock()
		defer j.mu.Unlock()
		if j.cancelled {
			return
		}
		j.pushLocked(data, m.bufferCap)
	}
	progress := func(cur string) {
		if m.store != nil && j.id != 0 {
			if err := m.store.UpdateCursor(j.id, cur); err != nil {
				m.onErr(fmt.Errorf("jobs: update cursor: %w", err))
			}
		}
	}

	runErr := j.kind.Run(j.ctx, params, cursor, emit, progress)

	var cancelData json.RawMessage
	hasCancel := false
	if ce, ok := j.kind.(cancelEventer); ok {
		cancelData, hasCancel = ce.CancelEvent()
	}

	cancelled := j.finalize(m.now(), cancelData, hasCancel, m.bufferCap)

	// OnComplete hook 在 finalize 之后,读权威 cancelled 状态,消除落历史与取消到达时刻的竞态。
	// 自然完成定义:未取消(cancelled=false)且无错(runErr=nil 或 kind 用错误承载结果)。
	// Hook 收到 runErr(可能是结果包装),由 kind 解包并决定是否落副作用。
	naturalCompletion := !cancelled && runErr != nil
	if naturalCompletion {
		if ch, ok := j.kind.(completionHooker); ok {
			ch.OnComplete(j.key, runErr)
		}
	}

	status := StatusDone
	switch {
	case cancelled:
		status = StatusCancelled
	case runErr != nil:
		// Hook 已调用,若它未消费 runErr 说明是真失败;若已消费(如 examResult)则是包装的自然完成,仍记 done。
		if naturalCompletion {
			if _, ok := j.kind.(completionHooker); ok {
				status = StatusDone
			} else {
				status = StatusFailed
			}
		} else {
			status = StatusFailed
		}
	}
	if m.store != nil && j.id != 0 {
		if err := m.store.Finish(j.id, status); err != nil {
			m.onErr(fmt.Errorf("jobs: persist finish: %w", err))
		}
	}
}

// RunningKeys 返回内存中进行中的指定 kind 任务的 key 列表。
// 持久化是尽力而为(Insert 失败退化为纯内存任务),做跨 key 互斥判断时
// 必须以内存态为准(DB 为辅),否则纯内存任务对互斥检查完全隐身。
func (m *Manager) RunningKeys(kind string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for id, j := range m.jobs {
		if id.kind != kind {
			continue
		}
		if !j.isDone() {
			keys = append(keys, id.key)
		}
	}
	return keys
}

// Cancel 取消(kind,key)任务:无任务或已收口返回 false。
func (m *Manager) Cancel(kind, key string) bool {
	m.mu.Lock()
	j, ok := m.jobs[jobID{kind: kind, key: key}]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return j.requestCancel()
}

// SweepExpired 清理超过 TTL 的已完成任务。
func (m *Manager) SweepExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(m.now())
}

// sweepLocked 移除已完成且超 TTL 的任务(调用方持有 m.mu)。
func (m *Manager) sweepLocked(now time.Time) {
	for id, j := range m.jobs {
		j.mu.Lock()
		expired := j.done && now.Sub(j.finishedAt) > m.ttl
		j.mu.Unlock()
		if expired {
			delete(m.jobs, id)
		}
	}
}

// Recover 重启恢复:加载所有 running 记录,可续跑的 kind 从游标续跑,
// 否则(单发任务或 kind 未注册)标记 interrupted。须在注册所有 kind 之后、对外服务之前调用。
func (m *Manager) Recover() error {
	return m.recover(false)
}

// RecoverOwn 只恢复本 Manager 已注册 kind 的遗留 running 任务,其他 kind 的记录原样跳过。
// Recover 会把未注册 kind 的 running 记录标 interrupted——多 Manager 共存时
// (retag/batch_detection/exam 各有 Manager)会误标别的运行时正在续跑的任务,
// 新 Manager 应一律用 RecoverOwn。
func (m *Manager) RecoverOwn() error {
	return m.recover(true)
}

func (m *Manager) recover(ownOnly bool) error {
	if m.store == nil {
		return nil
	}
	records, err := m.store.LoadRunning()
	if err != nil {
		return fmt.Errorf("jobs: load running: %w", err)
	}
	for _, rec := range records {
		m.mu.Lock()
		k, registered := m.kinds[rec.Kind]
		if !registered && ownOnly {
			m.mu.Unlock()
			continue
		}
		if !registered || !k.Resumable() {
			m.mu.Unlock()
			if err := m.store.Finish(rec.ID, StatusInterrupted); err != nil {
				m.onErr(fmt.Errorf("jobs: mark interrupted: %w", err))
			}
			continue
		}
		id := jobID{kind: rec.Kind, key: rec.Key}
		if _, exists := m.jobs[id]; exists {
			m.mu.Unlock()
			continue
		}
		j := m.newJob(k, rec.Key, rec.ID)
		m.jobs[id] = j
		params := rec.Params
		cursor := rec.Cursor
		m.mu.Unlock()
		go m.runJob(j, params, cursor)
	}
	return nil
}
