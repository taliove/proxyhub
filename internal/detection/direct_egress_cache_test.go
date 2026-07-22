package detection

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// countingDoHFixture 起一个计数的 mock DoH 服务(example.com -> 127.0.0.1),
// 返回 URL 与请求计数器(验证 singleflight 合并/缓存命中)。
func countingDoHFixture(t *testing.T, delay time.Duration) (string, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if delay > 0 {
			time.Sleep(delay)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read doh query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q := new(dns.Msg)
		if err := q.Unpack(body); err != nil {
			t.Errorf("unpack doh query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := new(dns.Msg)
		resp.SetReply(q)
		if q.Question[0].Qtype == dns.TypeA {
			resp.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("127.0.0.1").To4(),
			}}
		}
		packed, err := resp.Pack()
		if err != nil {
			t.Errorf("pack doh response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(packed)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &count
}

// TestDirectDialerCache_ReuseSameConfig 配置相同:复用同一拨号器实例
// (DoH 缓存跨节点命中、TLS 连接复用,不重复 net.Interfaces/probe)。
func TestDirectDialerCache_ReuseSameConfig(t *testing.T) {
	dohURL := dohFixture(t, nil)
	cfg := DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: loopbackIface()}

	var cache DirectDialerCache
	d1, err := cache.Get(cfg)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	d2, err := cache.Get(cfg)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if d1 != d2 {
		t.Error("Get() with same config returned different dialer instances, want memoized reuse")
	}
}

// TestDirectDialerCache_RebuildOnConfigChange 配置变更:重建拨号器(新实例),
// 不沿用旧配置的记忆化结果。
func TestDirectDialerCache_RebuildOnConfigChange(t *testing.T) {
	dohURL1 := dohFixture(t, nil)
	dohURL2 := dohFixture(t, nil)
	cfg1 := DirectEgressConfig{Enabled: true, DoHURL: dohURL1, Interface: loopbackIface()}
	cfg2 := DirectEgressConfig{Enabled: true, DoHURL: dohURL2, Interface: loopbackIface()}

	var cache DirectDialerCache
	d1, err := cache.Get(cfg1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	d2, err := cache.Get(cfg2)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if d1 == d2 {
		t.Error("Get() after config change returned the old dialer, want rebuild")
	}
	// 回到 cfg1 仍重建(缓存只记最新),不再展开断言;核心语义是"变才重建"。
}

// TestDirectDialerCache_CachesError 装配失败同样记忆化:同配置重复调用
// 直接返回缓存的错误(同配置必同错,不重复探测失败路径)。
func TestDirectDialerCache_CachesError(t *testing.T) {
	dohURL := dohFixture(t, nil)
	cfg := DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: "noexist0"}

	var cache DirectDialerCache
	if _, err := cache.Get(cfg); err == nil {
		t.Fatal("Get() expected error for nonexistent interface, got nil")
	}
	d, err := cache.Get(cfg)
	if err == nil {
		t.Fatal("Get() expected cached error, got nil")
	}
	if d != nil {
		t.Errorf("Get() dialer = %v, want nil on cached error", d)
	}
}

// TestDetectorDirectDialer_Memoized Detector 层:同一 provider 配置下两次取拨号器
// 返回同一实例;provider 返回值变化后重建。
func TestDetectorDirectDialer_Memoized(t *testing.T) {
	dohURL1 := dohFixture(t, nil)
	dohURL2 := dohFixture(t, nil)
	current := DirectEgressConfig{Enabled: true, DoHURL: dohURL1, Interface: loopbackIface()}

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig { return current })

	d1, err := d.directDialer()
	if err != nil {
		t.Fatalf("directDialer() error = %v", err)
	}
	d2, err := d.directDialer()
	if err != nil {
		t.Fatalf("directDialer() error = %v", err)
	}
	if d1 != d2 {
		t.Error("directDialer() with unchanged config returned different instances, want reuse")
	}

	current = DirectEgressConfig{Enabled: true, DoHURL: dohURL2, Interface: loopbackIface()}
	d3, err := d.directDialer()
	if err != nil {
		t.Fatalf("directDialer() after config change error = %v", err)
	}
	if d3 == d1 {
		t.Error("directDialer() after config change returned old instance, want rebuild")
	}
}

// TestDoHResolver_ConcurrentResolveSingleflight 拨号器共享后,同名并发解析
// 经 singleflight 合并:N 个 goroutine 同时解析同一域名只发一次 DoH 请求。
func TestDoHResolver_ConcurrentResolveSingleflight(t *testing.T) {
	dohURL, count := countingDoHFixture(t, 50*time.Millisecond)

	r, err := newDoHResolver(dohURL, nil)
	if err != nil {
		t.Fatalf("newDoHResolver() error = %v", err)
	}

	const goroutines = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, errs[idx] = r.resolve(ctx, "example.com")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("resolve() goroutine %d error = %v", i, err)
		}
	}
	if got := count.Load(); got != 1 {
		t.Errorf("DoH request count = %d, want 1 (singleflight must merge concurrent same-name resolves)", got)
	}
}

// TestDoHResolver_CacheHitSkipsDoH 缓存命中(未过期)不再发 DoH 请求。
func TestDoHResolver_CacheHitSkipsDoH(t *testing.T) {
	dohURL, count := countingDoHFixture(t, 0)

	r, err := newDoHResolver(dohURL, nil)
	if err != nil {
		t.Fatalf("newDoHResolver() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := r.resolve(ctx, "example.com"); err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if _, err := r.resolve(ctx, "example.com"); err != nil {
		t.Fatalf("resolve() second call error = %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Errorf("DoH request count = %d, want 1 (second resolve must hit cache)", got)
	}
}
