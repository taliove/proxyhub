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

// --- 并行组合段:出网探测与解锁判定同段并行,不串行 ---

func TestConcurrentStages_RunsSubStagesInParallel(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	blocking := func(name string) examStage {
		return examStage{name: name, run: func(_ context.Context, emit func(ExamEvent), _ *ExamReport) {
			started <- struct{}{}
			<-release
			emit(ExamEvent{Phase: "section_done", Section: name})
		}}
	}

	stage := concurrentStages("third", blocking("unlock"), blocking("egress"))

	var mu sync.Mutex
	var events []ExamEvent
	done := make(chan struct{})
	go func() {
		var report ExamReport
		stage.run(context.Background(), func(e ExamEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}, &report)
		close(done)
	}()

	// 两个子段必须同时在飞(串行则只有 1 个 started,超时判失败)。
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/2 sub-stages started concurrently: not parallel", i)
		}
	}
	close(release)
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 section_done", len(events))
	}
}

// 编排:出网信息段随第三段并入,事件仍在稳定性段收尾之后到达。
func TestOrchestrator_EgressAfterStability(t *testing.T) {
	egress := egressStage(EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7", Country: "United States"} },
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: true, Address: "2001:db8::1"} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example"} },
	}, time.Second)

	stability := examStage{name: "stability", run: func(_ context.Context, emit func(ExamEvent), report *ExamReport) {
		report.Stability = &StabilityMetrics{Succeeded: 1}
		emit(ExamEvent{Phase: "section_done", Section: "stability"})
	}}

	orch := &ExamOrchestrator{stages: []examStage{stability, concurrentStages("third", egress)}}

	var events []ExamEvent
	orch.Run(context.Background(), func(e ExamEvent) { events = append(events, e) })

	// 第一个事件必须是稳定性段收尾,出网事件在其后。
	if len(events) == 0 || events[0].Phase != "section_done" || events[0].Section != "stability" {
		t.Fatalf("first event = %+v, want stability section_done", events[0])
	}
	var firstEgressIdx = -1
	for i, e := range events {
		if e.Phase == "egress" || (e.Phase == "section_done" && e.Section == "egress") {
			firstEgressIdx = i
			break
		}
	}
	if firstEgressIdx <= 0 {
		t.Fatalf("egress events must appear after stability; events=%+v", events)
	}
}
