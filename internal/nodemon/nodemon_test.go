package nodemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSettings struct{ m map[string]string }

func (f fakeSettings) GetSetting(key string) (string, error) {
	if v, ok := f.m[key]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

type fakeSink struct {
	mu      sync.Mutex
	samples []Sample
}

func (f *fakeSink) SaveMonitorSample(nodeKey string, ok bool, latencyMs int, checkedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples = append(f.samples, Sample{NodeKey: nodeKey, OK: ok, LatencyMs: latencyMs, CheckedAt: checkedAt})
	return nil
}

func (f *fakeSink) PruneMonitorSamples(before time.Time) error { return nil }

type fakeProvider struct{ targets []Target }

func (f fakeProvider) MonitorTargets() []Target { return f.targets }

type fakeListener struct {
	mu    sync.Mutex
	count map[string]int // "userID|nodeKey" -> 次数
}

func (f *fakeListener) OnProbeResult(t Target, s Sample) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count == nil {
		f.count = make(map[string]int)
	}
	f.count[fmt.Sprintf("%d|%s", t.UserID, t.NodeKey)]++
}

type fakeAlerter struct {
	mu     sync.Mutex
	alerts []string
}

func (f *fakeAlerter) Alert(title, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, title+": "+content)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

// 起一个本地 TCP 监听,返回端口
func listenTCP(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestProbeTCP(t *testing.T) {
	port := listenTCP(t)
	ok, latency := probeTCP(context.Background(), "127.0.0.1", port, 2*time.Second)
	if !ok {
		t.Fatal("listening port should probe OK")
	}
	if latency < 0 {
		t.Errorf("latency = %d, want >= 0", latency)
	}

	// 关闭的端口:连接被拒 → 失败
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ok, _ = probeTCP(context.Background(), "127.0.0.1", closedPort, time.Second)
	if ok {
		t.Error("closed port should probe fail")
	}
}

func TestMonitorRound(t *testing.T) {
	upPort := listenTCP(t)
	sink := &fakeSink{}
	al := &fakeAlerter{}
	m := New(fakeSettings{m: map[string]string{"subscription_monitor_enabled": "true"}}, sink, al, testLogger())
	m.SetProvider(fakeProvider{targets: []Target{
		{UserID: 1, NodeKey: "up:1", Server: "127.0.0.1", Port: upPort},
		{UserID: 1, NodeKey: "down:1", Server: "127.0.0.1", Port: 1},    // 端口 1 不可连
		{UserID: 2, NodeKey: "up:1", Server: "127.0.0.1", Port: upPort}, // 跨用户重复:去重
	}})

	m.runRound(context.Background())

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.samples) != 2 {
		t.Fatalf("samples = %d, want 2 (dedupe)", len(sink.samples))
	}
	byKey := map[string]Sample{}
	for _, smp := range sink.samples {
		byKey[smp.NodeKey] = smp
	}
	if !byKey["up:1"].OK {
		t.Error("up:1 should be OK")
	}
	if byKey["down:1"].OK {
		t.Error("down:1 should fail")
	}
}

func TestMonitorDisabledNoRound(t *testing.T) {
	sink := &fakeSink{}
	m := New(fakeSettings{m: map[string]string{}}, sink, &fakeAlerter{}, testLogger())
	m.SetProvider(fakeProvider{targets: []Target{{NodeKey: "x:1", Server: "127.0.0.1", Port: 1}}})
	if m.enabled() {
		t.Fatal("missing setting should be disabled (zero regression)")
	}
	// Run 一轮即取消:开关关,不打点
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	m.Run(ctx)
	if len(sink.samples) != 0 {
		t.Errorf("disabled monitor should not probe, got %d samples", len(sink.samples))
	}
}

func TestMonitorFuseAlert(t *testing.T) {
	al := &fakeAlerter{}
	m := New(fakeSettings{m: map[string]string{"subscription_monitor_enabled": "true"}}, &fakeSink{}, al, testLogger())
	// 201 个目标(全部指向不可连端口,探测快败)
	targets := make([]Target, 0, 201)
	for i := 0; i < 201; i++ {
		targets = append(targets, Target{
			NodeKey: "n:" + strconv.Itoa(i), Server: "127.0.0.1", Port: 1,
		})
	}
	m.SetProvider(fakeProvider{targets: targets})

	m.runRound(context.Background())
	m.runRound(context.Background()) // 第二轮:不重复提示

	al.mu.Lock()
	defer al.mu.Unlock()
	if len(al.alerts) != 1 {
		t.Fatalf("fuse alerts = %d, want exactly 1", len(al.alerts))
	}
	if !strings.Contains(al.alerts[0], "监控集合过大") {
		t.Errorf("alert = %q, want fuse notice", al.alerts[0])
	}
}

func TestDedupeTargets(t *testing.T) {
	in := []Target{
		{UserID: 1, NodeKey: "a:1"}, {UserID: 2, NodeKey: "a:1"}, {UserID: 1, NodeKey: "b:1"},
	}
	out := dedupeTargets(in)
	if len(out) != 2 || out[0].NodeKey != "a:1" || out[1].NodeKey != "b:1" {
		t.Errorf("dedupe = %v", out)
	}
}

// TestRunRoundFanout 扇出分发(issue #100 评审):物理探测按 node_key 一次、
// 打点一次;listener 按 (用户,节点) 各收一次——同用户多地址命中同节点不重复计
func TestRunRoundFanout(t *testing.T) {
	port := listenTCP(t)
	sink := &fakeSink{}
	lis := &fakeListener{}
	m := New(fakeSettings{m: map[string]string{"subscription_monitor_enabled": "true"}}, sink, &fakeAlerter{}, testLogger())
	m.SetProvider(fakeProvider{targets: []Target{
		// 用户 1 的两个地址都含同一节点(全部 + 精选典型配置)
		{UserID: 1, NodeKey: "k:1", Server: "127.0.0.1", Port: port},
		{UserID: 1, NodeKey: "k:1", Server: "127.0.0.1", Port: port},
		// 用户 2 也含该节点
		{UserID: 2, NodeKey: "k:1", Server: "127.0.0.1", Port: port},
	}})
	m.SetListener(lis)

	m.runRound(context.Background())

	if len(sink.samples) != 1 {
		t.Errorf("samples = %d, want 1 (physical dedupe)", len(sink.samples))
	}
	lis.mu.Lock()
	defer lis.mu.Unlock()
	if len(lis.count) != 2 {
		t.Fatalf("listener dispatch = %v, want 2 (user,node) pairs", lis.count)
	}
	for k, c := range lis.count {
		if c != 1 {
			t.Errorf("%s dispatched %d times, want exactly 1 per round", k, c)
		}
	}
}
