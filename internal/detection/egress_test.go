package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// stubEgressTransport 按 host 把出网探测请求路由到预置正文,让模块化的 ProbeEgress
// 能经一个真实 *http.Client 端到端运行而不触网(测试出网信息的干净导出接口)。
type stubEgressTransport struct {
	byHost map[string]string
}

func (s stubEgressTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := s.byHost[r.URL.Host]
	if !ok {
		return nil, fmt.Errorf("no stub for host %q", r.URL.Host)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

// TestProbeEgress_ModularOverClient ProbeEgress 是不依赖体检编排(ExamOrchestrator/Stage)的
// 干净导出接口:任何调用方给一个经节点的 client 即可探 IPv4/IPv6/DNS 出口,内部完成
// 并行探测 + 网络类失败重试 + DNS 泄露接线(与体检段共用同一事实源)。
func TestProbeEgress_ModularOverClient(t *testing.T) {
	client := &http.Client{Transport: stubEgressTransport{byHost: map[string]string{
		// 脱敏:RFC5737 文档地址 + 示例 ASN;US 出口 vs Germany 解析器 -> 触发泄露判定。
		"ip-api.com": `{"status":"success","query":"203.0.113.7","country":"United States",` +
			`"countryCode":"US","as":"AS64500 Example Hosting LLC","org":"Example Hosting","hosting":true}`,
		"api6.ipify.org":  "2001:db8::1",
		"edns.ip-api.com": `{"dns":{"ip":"198.51.100.9","geo":"Germany - Example DNS"}}`,
	}}}

	got := ProbeEgress(context.Background(), client)

	if got.IPv4 == nil || got.IPv4.IP != "203.0.113.7" || got.IPv4.CountryCode != "US" {
		t.Fatalf("ipv4 = %+v, want 203.0.113.7/US", got.IPv4)
	}
	if got.IPv6 == nil || !got.IPv6.Available || got.IPv6.Address != "2001:db8::1" {
		t.Fatalf("ipv6 = %+v, want available 2001:db8::1", got.IPv6)
	}
	if got.DNS == nil || got.DNS.ResolverIP != "198.51.100.9" {
		t.Fatalf("dns = %+v, want resolver 198.51.100.9", got.DNS)
	}
	// 泄露判定在 ProbeEgress 内部接线,不需调用方额外拼装。
	if !got.DNS.Leak {
		t.Error("expected DNS leak flag (US exit vs Germany resolver) wired inside ProbeEgress")
	}
}

// TestProbeEgress_RetryPreservedInternally 16 的重试语义在模块化后保留:传输类失败(超时/EOF)
// 由 ProbeEgress 内部重试捞回,调用方无需自行包重试(单一事实源在 withEgressRetry)。
func TestProbeEgress_RetryPreservedInternally(t *testing.T) {
	tr := &flakyEgressTransport{byHost: map[string]string{
		"ip-api.com":      `{"status":"success","query":"203.0.113.7","country":"United States","countryCode":"US"}`,
		"api6.ipify.org":  "2001:db8::1",
		"edns.ip-api.com": `{"dns":{"ip":"198.51.100.9","geo":"United States - Example DNS"}}`,
	}, failFirstHost: "ip-api.com"}
	client := &http.Client{Transport: tr}

	got := ProbeEgress(context.Background(), client)

	if got.IPv4 == nil || got.IPv4.Error != "" || got.IPv4.IP != "203.0.113.7" {
		t.Fatalf("ipv4 = %+v, want recovered by internal retry", got.IPv4)
	}
	if tr.hits["ip-api.com"] != 2 {
		t.Errorf("ip-api.com hits = %d, want 2 (transient failure retried once)", tr.hits["ip-api.com"])
	}
}

// flakyEgressTransport 首次命中 failFirstHost 返回传输类错误(触发重试),其后返回预置正文。
// 三探针并行经同一 client,故 hits 计数以 mu 串行化(测试桩自身线程安全)。
type flakyEgressTransport struct {
	byHost        map[string]string
	failFirstHost string
	mu            sync.Mutex
	hits          map[string]int
}

func (f *flakyEgressTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	if f.hits == nil {
		f.hits = map[string]int{}
	}
	f.hits[r.URL.Host]++
	n := f.hits[r.URL.Host]
	f.mu.Unlock()
	if r.URL.Host == f.failFirstHost && n == 1 {
		return nil, fmt.Errorf("dial tcp: i/o timeout")
	}
	body, ok := f.byHost[r.URL.Host]
	if !ok {
		return nil, fmt.Errorf("no stub for host %q", r.URL.Host)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}
