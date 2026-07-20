package detection

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- IPv4 出口解析(纯函数) ---

func TestParseIPv4Egress(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, got EgressIPv4)
	}{
		{
			name: "success full fields",
			// ip-api.com/json 成功响应(脱敏:RFC5737 文档地址 + 示例 ASN)。
			body: `{"status":"success","query":"203.0.113.7","country":"United States",` +
				`"countryCode":"US","regionName":"California","city":"Los Angeles",` +
				`"as":"AS64500 Example Hosting LLC","org":"Example Hosting","isp":"Example ISP",` +
				`"proxy":true,"hosting":true,"mobile":false}`,
			check: func(t *testing.T, got EgressIPv4) {
				if got.IP != "203.0.113.7" {
					t.Errorf("IP = %q, want 203.0.113.7", got.IP)
				}
				if got.Country != "United States" || got.CountryCode != "US" {
					t.Errorf("country = %q/%q, want United States/US", got.Country, got.CountryCode)
				}
				if got.Region != "California" || got.City != "Los Angeles" {
					t.Errorf("region/city = %q/%q", got.Region, got.City)
				}
				if got.ASN != "AS64500" {
					t.Errorf("ASN = %q, want AS64500", got.ASN)
				}
				if got.Org != "Example Hosting" {
					t.Errorf("Org = %q, want Example Hosting", got.Org)
				}
				if !got.Proxy || !got.Hosting {
					t.Errorf("proxy/hosting = %v/%v, want true/true", got.Proxy, got.Hosting)
				}
			},
		},
		{
			name: "org falls back to isp when org empty",
			body: `{"status":"success","query":"203.0.113.8","country":"Japan","countryCode":"JP",` +
				`"as":"AS64501 Example Telecom","org":"","isp":"Example Telecom ISP","proxy":false,"hosting":false}`,
			check: func(t *testing.T, got EgressIPv4) {
				if got.Org != "Example Telecom ISP" {
					t.Errorf("Org = %q, want isp fallback Example Telecom ISP", got.Org)
				}
				if got.Proxy || got.Hosting {
					t.Errorf("proxy/hosting = %v/%v, want false/false", got.Proxy, got.Hosting)
				}
			},
		},
		{
			name:    "api reports fail status -> error (no misjudge)",
			body:    `{"status":"fail","message":"private range","query":"10.0.0.1"}`,
			wantErr: true,
		},
		{
			name:    "malformed json -> error",
			body:    `{not json`,
			wantErr: true,
		},
		{
			name:    "empty body -> error",
			body:    ``,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPv4Egress(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (result %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

// --- IPv6 出口探活(纯函数):区分有出口 / 不可达 / 解析失败 ---

func TestClassifyIPv6Egress(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		reqErr        error
		wantAvailable bool
		wantAddress   string
		wantErr       bool // Error 字段是否非空
	}{
		{
			name:          "valid ipv6 -> available with address",
			body:          "2001:db8::1\n",
			wantAvailable: true,
			wantAddress:   "2001:db8::1",
		},
		{
			name:          "unreachable (dial error) -> no ipv6 egress, not an error",
			reqErr:        errors.New("dial tcp: no route to host"),
			wantAvailable: false,
			wantErr:       false, // 拨号失败是明确的"无 IPv6 出口",非探测异常
		},
		{
			name:          "segment timeout -> probe error, not a definite no-egress verdict",
			reqErr:        fmt.Errorf("get: %w", context.DeadlineExceeded),
			wantAvailable: false,
			wantErr:       true, // 超时未测完,不当作"无 IPv6 出口"结论
		},
		{
			name:          "reachable but garbage body -> parse failure (distinct from unreachable)",
			body:          "<html>error</html>",
			wantAvailable: false,
			wantErr:       true, // 解析失败要与不可达区分:带 Error
		},
		{
			name:          "ipv4 in body is not ipv6 -> parse failure",
			body:          "203.0.113.7",
			wantAvailable: false,
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyIPv6Egress(tt.body, tt.reqErr)
			if got.Available != tt.wantAvailable {
				t.Errorf("Available = %v, want %v", got.Available, tt.wantAvailable)
			}
			if got.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", got.Address, tt.wantAddress)
			}
			if (got.Error != "") != tt.wantErr {
				t.Errorf("Error = %q, want non-empty=%v", got.Error, tt.wantErr)
			}
		})
	}
}

// --- DNS 出口解析(纯函数) ---

func TestParseDNSEgress(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantErr    bool
		wantIP     string
		wantGeo    string
	}{
		{
			name:    "success",
			body:    `{"dns":{"ip":"198.51.100.9","geo":"United States - Example DNS"}}`,
			wantIP:  "198.51.100.9",
			wantGeo: "United States - Example DNS",
		},
		{
			name:    "malformed json -> error",
			body:    `{"dns":`,
			wantErr: true,
		},
		{
			name:    "missing resolver ip -> error",
			body:    `{"dns":{"ip":"","geo":""}}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDNSEgress(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ResolverIP != tt.wantIP || got.ResolverGeo != tt.wantGeo {
				t.Errorf("got %q/%q, want %q/%q", got.ResolverIP, got.ResolverGeo, tt.wantIP, tt.wantGeo)
			}
			if got.Leak {
				t.Error("parseDNSEgress must not set Leak (leak is decided against exit country)")
			}
		})
	}
}

// --- DNS 泄露判定(纯函数):一致 / 不一致 / 无法判定 ---

func TestDetectDNSLeak(t *testing.T) {
	tests := []struct {
		name        string
		exitCountry string
		resolverGeo string
		want        bool
	}{
		{name: "consistent country -> no leak", exitCountry: "United States", resolverGeo: "United States - Example DNS", want: false},
		{name: "case insensitive consistent", exitCountry: "united states", resolverGeo: "United States - Example DNS", want: false},
		{name: "inconsistent country -> leak", exitCountry: "United States", resolverGeo: "Germany - Example DNS", want: true},
		{name: "geo without org segment consistent", exitCountry: "Japan", resolverGeo: "Japan", want: false},
		{name: "empty resolver geo -> undetermined (no leak)", exitCountry: "United States", resolverGeo: "", want: false},
		{name: "empty exit country -> undetermined (no leak)", exitCountry: "", resolverGeo: "Germany - Example DNS", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDNSLeak(tt.exitCountry, tt.resolverGeo); got != tt.want {
				t.Errorf("detectDNSLeak(%q,%q) = %v, want %v", tt.exitCountry, tt.resolverGeo, got, tt.want)
			}
		})
	}
}

// --- 出网信息段:并行探测三类,逐条推送 + 段末聚合 + 泄露判定接线 ---

func TestEgressStage_EmitsRowsThenSectionDone(t *testing.T) {
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 {
			return EgressIPv4{IP: "203.0.113.7", Country: "United States", CountryCode: "US", Hosting: true}
		},
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS: func(context.Context) EgressDNS {
			return EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "Germany - Example DNS"}
		},
	}

	stage := egressStage(probe, time.Second)
	if stage.name != "egress" {
		t.Fatalf("stage name = %q, want egress", stage.name)
	}

	var report ExamReport
	var rows, sectionDone int
	var mu sync.Mutex
	stage.run(context.Background(), func(e ExamEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Phase {
		case "egress":
			rows++
			if e.Section != "egress" || e.Egress == nil {
				t.Errorf("bad egress row event: %+v", e)
			}
		case "section_done":
			sectionDone++
			if e.Section != "egress" || e.Egress == nil {
				t.Errorf("section_done missing egress metrics: %+v", e)
			}
		}
	}, &report)

	if rows != 3 {
		t.Errorf("egress row events = %d, want 3 (ipv4/ipv6/dns)", rows)
	}
	if sectionDone != 1 {
		t.Errorf("section_done events = %d, want 1", sectionDone)
	}
	if report.Egress == nil || report.Egress.IPv4 == nil || report.Egress.IPv6 == nil || report.Egress.DNS == nil {
		t.Fatalf("report.Egress incomplete: %+v", report.Egress)
	}
	// US 出口 vs Germany 解析器 -> 疑似 DNS 泄露(聚合时接线判定)。
	if !report.Egress.DNS.Leak {
		t.Error("expected DNS leak flag (US exit vs Germany resolver)")
	}
}

// egressErrorStage 降级段:探测器构造失败时,三类仍逐条推 error 行 + section_done,不静默跳过。
func TestEgressErrorStage_EmitsErrorRows(t *testing.T) {
	stage := egressErrorStage(errors.New("proxy session failed"))
	if stage.name != "egress" {
		t.Fatalf("stage name = %q, want egress", stage.name)
	}

	var report ExamReport
	var rows, sectionDone int
	stage.run(context.Background(), func(e ExamEvent) {
		switch e.Phase {
		case "egress":
			rows++
		case "section_done":
			sectionDone++
		}
	}, &report)

	if rows != 3 {
		t.Errorf("error rows = %d, want 3", rows)
	}
	if sectionDone != 1 {
		t.Errorf("section_done = %d, want 1", sectionDone)
	}
	if report.Egress == nil || report.Egress.IPv4 == nil || report.Egress.IPv4.Error == "" {
		t.Fatalf("expected ipv4 error result, got %+v", report.Egress)
	}
}

// --- 出网重试:网络类失败重试;明确负判定 / 解析失败不重试 ---

// TestWithEgressRetry_IPv4TransientRetried IPv4 传输失败先失败后成功:重试捞回。
func TestWithEgressRetry_IPv4TransientRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 {
			calls++
			if calls == 1 {
				return EgressIPv4{Error: "请求失败: dial tcp: i/o timeout"}
			}
			return EgressIPv4{IP: "203.0.113.7", Country: "United States"}
		},
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	res := withEgressRetry(probe).IPv4(context.Background())
	if res.Error != "" || res.IP != "203.0.113.7" {
		t.Errorf("ipv4 = %+v, want recovered", res)
	}
	if calls != 2 {
		t.Errorf("ipv4 calls = %d, want 2", calls)
	}
}

// TestWithEgressRetry_IPv4ParseFailNotRetried 解析失败(拿到响应但非法)非传输类,不重试。
func TestWithEgressRetry_IPv4ParseFailNotRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 {
			calls++
			return EgressIPv4{Error: "解析 IPv4 出口响应失败: invalid character"}
		},
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	withEgressRetry(probe).IPv4(context.Background())
	if calls != 1 {
		t.Errorf("parse failure retried (%d calls), want 1", calls)
	}
}

// TestWithEgressRetry_IPv6TimeoutRetried IPv6 探测超时(传输类)重试;成功即返回。
func TestWithEgressRetry_IPv6TimeoutRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7"} },
		IPv6: func(context.Context) EgressIPv6 {
			calls++
			if calls == 1 {
				return EgressIPv6{Available: false, Error: "IPv6 出口探测超时"}
			}
			return EgressIPv6{Available: true, Address: "2001:db8::1"}
		},
		DNS: func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	res := withEgressRetry(probe).IPv6(context.Background())
	if !res.Available || res.Address != "2001:db8::1" {
		t.Errorf("ipv6 = %+v, want recovered available", res)
	}
	if calls != 2 {
		t.Errorf("ipv6 calls = %d, want 2", calls)
	}
}

// TestWithEgressRetry_IPv6UnreachableNotRetried 不可达是明确负判定(Error 空),绝不重试。
func TestWithEgressRetry_IPv6UnreachableNotRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7"} },
		IPv6: func(context.Context) EgressIPv6 {
			calls++
			return EgressIPv6{Available: false} // 明确无 IPv6 出口
		},
		DNS: func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	res := withEgressRetry(probe).IPv6(context.Background())
	if res.Available {
		t.Error("unreachable must stay negative")
	}
	if calls != 1 {
		t.Errorf("definite no-egress retried (%d calls), want 1", calls)
	}
}

// TestWithEgressRetry_DNSTransientRetried DNS 传输失败重试捞回。
func TestWithEgressRetry_DNSTransientRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7"} },
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS: func(context.Context) EgressDNS {
			calls++
			if calls == 1 {
				return EgressDNS{Error: "请求失败: unexpected EOF"}
			}
			return EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example DNS"}
		},
	}
	res := withEgressRetry(probe).DNS(context.Background())
	if res.Error != "" || res.ResolverIP != "198.51.100.9" {
		t.Errorf("dns = %+v, want recovered", res)
	}
	if calls != 2 {
		t.Errorf("dns calls = %d, want 2", calls)
	}
}

// TestWithEgressRetry_SuccessNoRetry 三类首探即成功:各只调用一次。
func TestWithEgressRetry_SuccessNoRetry(t *testing.T) {
	var v4, v6, dns int
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { v4++; return EgressIPv4{IP: "203.0.113.7"} },
		IPv6: func(context.Context) EgressIPv6 { v6++; return EgressIPv6{Available: true, Address: "2001:db8::1"} },
		DNS:  func(context.Context) EgressDNS { dns++; return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	wrapped := withEgressRetry(probe)
	wrapped.IPv4(context.Background())
	wrapped.IPv6(context.Background())
	wrapped.DNS(context.Background())
	if v4 != 1 || v6 != 1 || dns != 1 {
		t.Errorf("success calls = %d/%d/%d, want 1/1/1", v4, v6, dns)
	}
}

// 编排级的段序(出网前置)由 TestExamStream_StageOrder 覆盖;出网段自身行为见 TestEgressStage_*。
