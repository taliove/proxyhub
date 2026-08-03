package region

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingDeps 计数假 deps:断言三层短路(前层命中后层不触发)与缓存命中不重复 DNS。
type countingDeps struct {
	nameCalls    atomic.Int32
	lookupCalls  atomic.Int32
	countryCalls atomic.Int32
	putCalls     atomic.Int32

	nameCode    string              // L1 返回值("" 视同未命中)
	nameFunc    func(string) string // 非 nil 时优先于 nameCode(按名字分流)
	lookupIPs   []string            // L3 DNS 返回值
	lookupErr   error
	countryCode string
	countryErr  error

	cache     map[string]string // host -> code(空串 = 负缓存)
	cacheTime time.Time
}

func (d *countingDeps) deps() Deps {
	return Deps{
		RecognizeName: func(name string) string {
			d.nameCalls.Add(1)
			if d.nameFunc != nil {
				return d.nameFunc(name)
			}
			return d.nameCode
		},
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			d.lookupCalls.Add(1)
			return d.lookupIPs, d.lookupErr
		},
		LookupCountry: func(ip string) (string, error) {
			d.countryCalls.Add(1)
			return d.countryCode, d.countryErr
		},
		GetCached: func(host string) (string, time.Time, bool) {
			if d.cache == nil {
				return "", time.Time{}, false
			}
			code, ok := d.cache[host]
			return code, d.cacheTime, ok
		},
		PutCached: func(host, code string) {
			d.putCalls.Add(1)
			if d.cache == nil {
				d.cache = map[string]string{}
			}
			d.cache[host] = code
			d.cacheTime = time.Now()
		},
	}
}

func TestRecognize_L1HitSkipsL2L3(t *testing.T) {
	d := &countingDeps{nameCode: "HK"}
	r := New(d.deps())

	// 名字同时带国旗 emoji 与可解析域名:L1 命中后 L2/L3 都不得触发
	if got := r.Recognize(context.Background(), "🇯🇵 香港01", "example.com"); got != "HK" {
		t.Errorf("Recognize() = %q, want HK", got)
	}
	if d.lookupCalls.Load() != 0 || d.countryCalls.Load() != 0 {
		t.Errorf("L1 hit but L3 fired: lookup=%d country=%d, want 0/0", d.lookupCalls.Load(), d.countryCalls.Load())
	}
}

func TestRecognize_L2HitSkipsL3(t *testing.T) {
	// L1 未命中(返回 "Unknown"),名字带规则表未覆盖的小众地区国旗:L2 命中,L3 不触发
	d := &countingDeps{nameCode: Unknown}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "🇲🇻 马尔代夫 01", "example.com"); got != "MV" {
		t.Errorf("Recognize() = %q, want MV", got)
	}
	if d.nameCalls.Load() != 1 {
		t.Errorf("L1 calls = %d, want 1", d.nameCalls.Load())
	}
	if d.lookupCalls.Load() != 0 || d.countryCalls.Load() != 0 {
		t.Errorf("L2 hit but L3 fired: lookup=%d country=%d, want 0/0", d.lookupCalls.Load(), d.countryCalls.Load())
	}
}

func TestRecognize_L3DomainGeoIP(t *testing.T) {
	// 前两层全未命中:L3 域名 -> DNS -> GeoIP
	d := &countingDeps{
		nameCode:    Unknown,
		lookupIPs:   []string{"203.0.113.7"},
		countryCode: "BT",
	}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != "BT" {
		t.Errorf("Recognize() = %q, want BT", got)
	}
	if d.lookupCalls.Load() != 1 || d.countryCalls.Load() != 1 {
		t.Errorf("L3 calls = lookup:%d country:%d, want 1/1", d.lookupCalls.Load(), d.countryCalls.Load())
	}
	// 正结果写缓存
	if d.putCalls.Load() != 1 || d.cache["example.com"] != "BT" {
		t.Errorf("cache put = %d calls, cache=%v, want 1 call with BT", d.putCalls.Load(), d.cache)
	}
}

func TestRecognize_L3DNSFailureDegrades(t *testing.T) {
	d := &countingDeps{nameCode: Unknown, lookupErr: errors.New("doh timeout")}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != Unknown {
		t.Errorf("Recognize() = %q, want Unknown on DNS failure", got)
	}
	if d.countryCalls.Load() != 0 {
		t.Errorf("DNS failed but GeoIP fired: country=%d, want 0", d.countryCalls.Load())
	}
	// 负缓存:防每轮刷新重试
	if d.putCalls.Load() != 1 {
		t.Errorf("negative cache put = %d, want 1", d.putCalls.Load())
	}
	if code, ok := d.cache["example.com"]; !ok || code != "" {
		t.Errorf("negative cache row = (%q, %v), want (\"\", true)", code, ok)
	}
}

func TestRecognize_L3CancelledCtxSkipsNegativeCache(t *testing.T) {
	// 父 ctx 已取消(刷新被取消路径):LookupHost 立即失败,但这不是对端事实,
	// 不得写负缓存——否则下轮刷新在 1h 内对该 host 直接误判 Unknown。
	d := &countingDeps{nameCode: Unknown, lookupErr: context.Canceled}
	r := New(d.deps())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := r.Recognize(ctx, "节点 01", "example.com"); got != Unknown {
		t.Errorf("Recognize() = %q, want Unknown on cancelled ctx", got)
	}
	if d.putCalls.Load() != 0 {
		t.Errorf("negative cache put on cancelled ctx = %d, want 0 (local cancel is not a peer fact)", d.putCalls.Load())
	}
}

func TestRecognize_L3PrivateIPDegrades(t *testing.T) {
	// DNS 成功但解析到私网 IP:GeoIP 无记录,降级 Unknown(写负缓存)
	d := &countingDeps{
		nameCode:   Unknown,
		lookupIPs:  []string{"10.0.0.1"},
		countryErr: errors.New("geoip: no country for ip"),
	}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != Unknown {
		t.Errorf("Recognize() = %q, want Unknown for private IP", got)
	}
	if d.countryCalls.Load() != 1 {
		t.Errorf("country calls = %d, want 1", d.countryCalls.Load())
	}
	if d.putCalls.Load() != 1 {
		t.Errorf("negative cache put = %d, want 1", d.putCalls.Load())
	}
}

func TestRecognize_L3IPLiteralSkipsDNS(t *testing.T) {
	// server 是 IP 字面量:零网络,直接离线查库
	d := &countingDeps{nameCode: Unknown, countryCode: "US"}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "203.0.113.9"); got != "US" {
		t.Errorf("Recognize() = %q, want US", got)
	}
	if d.lookupCalls.Load() != 0 {
		t.Errorf("IP literal triggered DNS: lookup=%d, want 0", d.lookupCalls.Load())
	}
	if d.putCalls.Load() != 0 {
		t.Errorf("IP literal wrote cache: put=%d, want 0 (offline lookup has zero cost)", d.putCalls.Load())
	}
}

func TestRecognize_CacheHitSkipsDNS(t *testing.T) {
	d := &countingDeps{nameCode: Unknown, lookupIPs: []string{"203.0.113.7"}, countryCode: "BT"}
	r := New(d.deps())

	// 第一次:DNS + 写缓存
	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != "BT" {
		t.Fatalf("first Recognize() = %q, want BT", got)
	}
	// 第二次:缓存命中,不再 DNS
	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != "BT" {
		t.Fatalf("second Recognize() = %q, want BT", got)
	}
	if d.lookupCalls.Load() != 1 {
		t.Errorf("lookup calls = %d, want 1 (cache hit must skip DNS)", d.lookupCalls.Load())
	}
}

func TestRecognize_ExpiredCacheResolves(t *testing.T) {
	d := &countingDeps{
		nameCode:    Unknown,
		lookupIPs:   []string{"203.0.113.7"},
		countryCode: "BT",
		// 预置一条 8 天前的正缓存:已过 7 天 TTL,必须重查
		cache:     map[string]string{"example.com": "US"},
		cacheTime: time.Now().Add(-8 * 24 * time.Hour),
	}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != "BT" {
		t.Errorf("Recognize() = %q, want BT (expired cache must re-resolve)", got)
	}
	if d.lookupCalls.Load() != 1 {
		t.Errorf("lookup calls = %d, want 1", d.lookupCalls.Load())
	}
}

func TestRecognize_FreshNegativeCacheSkipsDNS(t *testing.T) {
	d := &countingDeps{
		nameCode:  Unknown,
		cache:     map[string]string{"example.com": ""}, // 10 分钟前的负缓存,1h TTL 内
		cacheTime: time.Now().Add(-10 * time.Minute),
	}
	r := New(d.deps())

	if got := r.Recognize(context.Background(), "节点 01", "example.com"); got != Unknown {
		t.Errorf("Recognize() = %q, want Unknown", got)
	}
	if d.lookupCalls.Load() != 0 {
		t.Errorf("fresh negative cache but DNS fired: lookup=%d, want 0", d.lookupCalls.Load())
	}
}

func TestRecognizeBatch_DedupesHosts(t *testing.T) {
	d := &countingDeps{
		nameFunc: func(name string) string {
			// 模拟真实 L1:名字含"香港"命中 HK,其余未命中
			if strings.Contains(name, "香港") {
				return "HK"
			}
			return Unknown
		},
		lookupIPs:   []string{"203.0.113.7"},
		countryCode: "BT",
	}
	r := New(d.deps())

	reqs := []Request{
		{Name: "香港 01", Server: "hk-node.example.com"}, // L1 命中
		{Name: "节点 02", Server: "example.com"},         // L3
		{Name: "节点 03", Server: "example.com"},         // L3 同 host:去重
		{Name: "🇧🇹 不丹 04", Server: "btn.example.com"},  // L2 命中
	}
	got := r.RecognizeBatch(context.Background(), reqs)
	want := []string{"HK", "BT", "BT", "BT"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("out[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if d.lookupCalls.Load() != 1 {
		t.Errorf("lookup calls = %d, want 1 (same host must resolve once)", d.lookupCalls.Load())
	}
}

func TestRecognize_NilDepsDegrade(t *testing.T) {
	// 全 nil deps(极端降级):不 panic,L2 仍可用,其余 Unknown
	r := New(Deps{})
	if got := r.Recognize(context.Background(), "🇯🇵 节点", "example.com"); got != "JP" {
		t.Errorf("Recognize() = %q, want JP (L2 always available)", got)
	}
	if got := r.Recognize(context.Background(), "节点", "example.com"); got != Unknown {
		t.Errorf("Recognize() = %q, want Unknown", got)
	}
}
