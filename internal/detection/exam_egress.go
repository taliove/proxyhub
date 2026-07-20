package detection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 出网信息段默认参数。三类小请求(IPv4/IPv6/DNS)并行,整段共享一个总超时。
const egressSegmentTimeoutSec = 15

// egressSegmentTimeout 出网信息段总超时:到点经 ctx 取消在飞探测,各探测随即以 error/不可达返回。
const egressSegmentTimeout = egressSegmentTimeoutSec * time.Second

// 出网信息探测端点。ip-api 免费版仅 HTTP(经节点出网,考察的是节点出口而非本地链路)。
const (
	// egressIPv4URL ip-api.com/json 经节点请求 -> 出口 IPv4 地址 / 地区 / ASN / proxy-hosting 标记。
	egressIPv4URL = "http://ip-api.com/json/?fields=status,message,query,country,countryCode," +
		"regionName,city,as,asname,isp,org,proxy,hosting,mobile"
	// egressIPv6URL v6-only 端点(仅解析 AAAA):经节点可达则有 IPv6 出口,不可达即无。
	egressIPv6URL = "https://api6.ipify.org?format=text"
	// egressDNSURL edns.ip-api.com 经节点请求 -> 出口 DNS 解析器 IP 及归属地。
	egressDNSURL = "http://edns.ip-api.com/json"
)

// EgressIPv4 IPv4 出口信息:成功含地址/地区/ASN/标记,失败仅 Error 非空。
type EgressIPv4 struct {
	IP          string `json:"ip,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
	ASN         string `json:"asn,omitempty"` // 自治域号(如 AS64500)
	Org         string `json:"org,omitempty"` // 运营商/机房(org 优先,回退 isp)
	Proxy       bool   `json:"proxy"`         // 疑似代理/VPN 出口
	Hosting     bool   `json:"hosting"`       // 机房/数据中心 IP(非住宅)
	Error       string `json:"error,omitempty"`
}

// EgressIPv6 IPv6 出口信息:Available 有出口且 Address 非空;不可达则 Available=false 且 Error 空;
// 解析失败(拿到响应但非合法 v6)则 Available=false 且 Error 非空 —— 三态可区分,不可达不误判为异常。
type EgressIPv6 struct {
	Available bool   `json:"available"`
	Address   string `json:"address,omitempty"`
	Error     string `json:"error,omitempty"`
}

// EgressDNS 出口 DNS 信息:解析器 IP 与归属地;Leak 为与出口国家不一致的疑似 DNS 泄露标记。
type EgressDNS struct {
	ResolverIP  string `json:"resolver_ip,omitempty"`
	ResolverGeo string `json:"resolver_geo,omitempty"` // 归属地(如 "United States - Example DNS")
	Leak        bool   `json:"leak"`                   // 疑似 DNS 泄露(解析器国家 != 出口国家)
	Error       string `json:"error,omitempty"`
}

// EgressMetrics 出网信息段聚合结果:三类各一份(探测失败时对应子项的 Error 非空)。
type EgressMetrics struct {
	IPv4 *EgressIPv4 `json:"ipv4,omitempty"`
	IPv6 *EgressIPv6 `json:"ipv6,omitempty"`
	DNS  *EgressDNS  `json:"dns,omitempty"`
}

// EgressProbe 出网信息三类探测器:各自经同一节点会话请求对应端点并解析(测试可注入假探测器)。
type EgressProbe struct {
	IPv4 func(ctx context.Context) EgressIPv4
	IPv6 func(ctx context.Context) EgressIPv6
	DNS  func(ctx context.Context) EgressDNS
}

// ipAPIResponse ip-api.com/json 响应(仅取用到的字段)。
type ipAPIResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	Query      string `json:"query"`
	Country    string `json:"country"`
	CountryeCd string `json:"countryCode"`
	RegionName string `json:"regionName"`
	City       string `json:"city"`
	As         string `json:"as"`
	Asname     string `json:"asname"`
	Isp        string `json:"isp"`
	Org        string `json:"org"`
	Proxy      bool   `json:"proxy"`
	Hosting    bool   `json:"hosting"`
	Mobile     bool   `json:"mobile"`
}

// parseIPv4Egress 解析 ip-api.com/json 正文为出口信息。
// 非法 JSON 或 status!=success 一律返回 error(交由上层落 Error 字段),绝不把失败误判为可用出口。
func parseIPv4Egress(body string) (EgressIPv4, error) {
	var r ipAPIResponse
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return EgressIPv4{}, fmt.Errorf("解析 IPv4 出口响应失败: %w", err)
	}
	if r.Status != "success" {
		msg := r.Message
		if msg == "" {
			msg = "lookup failed"
		}
		return EgressIPv4{}, fmt.Errorf("IPv4 出口查询失败: %s", msg)
	}
	return EgressIPv4{
		IP:          r.Query,
		Country:     r.Country,
		CountryCode: r.CountryeCd,
		Region:      r.RegionName,
		City:        r.City,
		ASN:         asnToken(r.As),
		Org:         firstNonEmpty(r.Org, r.Isp, r.Asname),
		Proxy:       r.Proxy,
		Hosting:     r.Hosting,
	}, nil
}

// asnToken 从 ip-api 的 as 字段("AS64500 Example Org")取前导 ASN 号;缺失返回空。
func asnToken(as string) string {
	fields := strings.Fields(as)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// firstNonEmpty 返回首个非空(去空格后)字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// classifyIPv6Egress 由 v6-only 端点响应判定 IPv6 出口三态:
// 超时/取消 -> 探测未完成(Error 非空,不当作"无出口"结论);
// 其余请求错误(拨号失败/无路由)-> 无 IPv6 出口(不可达,明确负判定,Error 留空);
// 合法 v6 正文 -> 有出口给地址;拿到响应但非合法 v6 -> 解析失败(Error 非空,与不可达区分)。
func classifyIPv6Egress(body string, reqErr error) EgressIPv6 {
	if reqErr != nil {
		// 超时/取消不是"节点无 IPv6"的证据(可能只是本轮没测完),标为探测异常而非负判定。
		if errors.Is(reqErr, context.DeadlineExceeded) || errors.Is(reqErr, context.Canceled) {
			return EgressIPv6{Available: false, Error: "IPv6 出口探测超时"}
		}
		return EgressIPv6{Available: false}
	}
	addr := strings.TrimSpace(body)
	ip := net.ParseIP(addr)
	if ip != nil && ip.To4() == nil && strings.Contains(addr, ":") {
		return EgressIPv6{Available: true, Address: addr}
	}
	return EgressIPv6{Available: false, Error: "无法解析 IPv6 出口响应"}
}

// ednsResponse edns.ip-api.com/json 响应(仅取 dns 子对象)。
type ednsResponse struct {
	DNS struct {
		IP  string `json:"ip"`
		Geo string `json:"geo"`
	} `json:"dns"`
}

// parseDNSEgress 解析 edns.ip-api.com 正文为解析器 IP 与归属地(不判泄露,泄露由 detectDNSLeak 对出口国家判定)。
// 非法 JSON 或缺解析器 IP 一律返回 error。
func parseDNSEgress(body string) (EgressDNS, error) {
	var r ednsResponse
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return EgressDNS{}, fmt.Errorf("解析 DNS 出口响应失败: %w", err)
	}
	if strings.TrimSpace(r.DNS.IP) == "" {
		return EgressDNS{}, fmt.Errorf("DNS 出口响应缺解析器地址")
	}
	return EgressDNS{ResolverIP: r.DNS.IP, ResolverGeo: r.DNS.Geo}, nil
}

// detectDNSLeak 判定疑似 DNS 泄露:解析器归属国与出口国不一致即泄露。
// 任一方国家未知(空)则无法判定,返回 false(不制造假阳性)。
func detectDNSLeak(exitCountry, resolverGeo string) bool {
	exit := strings.TrimSpace(exitCountry)
	resolverCountry := strings.TrimSpace(dnsGeoCountry(resolverGeo))
	if exit == "" || resolverCountry == "" {
		return false
	}
	return !strings.EqualFold(resolverCountry, exit)
}

// dnsGeoCountry 从 edns geo("United States - Example DNS")取前导国家名;无分隔符则整串为国家名。
func dnsGeoCountry(geo string) string {
	if geo == "" {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(geo, " - ", 2)[0])
}

// egressStage 构造出网信息段:三类小请求并行探测,每完成一类推一行 egress,
// 段末按出口国家接线 DNS 泄露判定,推 section_done + 聚合指标。
func egressStage(probe EgressProbe, timeout time.Duration) examStage {
	return examStage{
		name: "egress",
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			ectx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			agg := runEgressProbes(ectx, probe, func(partial EgressMetrics) {
				pc := partial
				emit(ExamEvent{Phase: "egress", Section: "egress", Egress: &pc})
			})

			// 泄露判定在聚合时接线:需出口国家(IPv4)与解析器归属(DNS)都到位。
			// 不可变地写入新副本:逐条 egress 帧已发出 agg.DNS 的指针别名(仍可能在 SSE 缓冲中被并发 marshal),
			// 原地改 Leak 会与 handler 的并发编码抢写同一字段;换成新对象消除该竞态。
			if agg.DNS != nil && agg.IPv4 != nil {
				dnsWithLeak := *agg.DNS
				dnsWithLeak.Leak = detectDNSLeak(agg.IPv4.Country, agg.DNS.ResolverGeo)
				agg.DNS = &dnsWithLeak
			}
			report.Egress = &agg
			ac := agg
			emit(ExamEvent{Phase: "section_done", Section: "egress", Egress: &ac})
		},
	}
}

// runEgressProbes 并行跑三类探测,按完成顺序逐个回调 onPartial(在收集 goroutine 内串行触发,
// 保证 emit 线程安全);返回三类聚合结果(Leak 尚未接线,由调用方按出口国家判定)。
func runEgressProbes(ctx context.Context, probe EgressProbe, onPartial func(EgressMetrics)) EgressMetrics {
	ch := make(chan EgressMetrics, 3)
	go func() { ch <- EgressMetrics{IPv4: ptrIPv4(probe.IPv4(ctx))} }()
	go func() { ch <- EgressMetrics{IPv6: ptrIPv6(probe.IPv6(ctx))} }()
	go func() { ch <- EgressMetrics{DNS: ptrDNS(probe.DNS(ctx))} }()

	var agg EgressMetrics
	for i := 0; i < 3; i++ {
		part := <-ch
		mergeEgress(&agg, part)
		if onPartial != nil {
			onPartial(part)
		}
	}
	return agg
}

// mergeEgress 把一类探测结果叠加进聚合(仅覆盖该类的非空子项)。
func mergeEgress(dst *EgressMetrics, part EgressMetrics) {
	if part.IPv4 != nil {
		dst.IPv4 = part.IPv4
	}
	if part.IPv6 != nil {
		dst.IPv6 = part.IPv6
	}
	if part.DNS != nil {
		dst.DNS = part.DNS
	}
}

func ptrIPv4(v EgressIPv4) *EgressIPv4 { return &v }
func ptrIPv6(v EgressIPv6) *EgressIPv6 { return &v }
func ptrDNS(v EgressDNS) *EgressDNS    { return &v }

// egressErrorStage 降级段:探测器构造失败(如无法建立节点会话)时,三类各推一行 error 而非静默跳过,
// 使段的缺失对用户可解释(段末仍推 section_done)。
func egressErrorStage(cause error) examStage {
	msg := fmt.Sprintf("出网信息探测初始化失败: %v", cause)
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{Error: msg} },
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false, Error: msg} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{Error: msg} },
	}
	return egressStage(probe, egressSegmentTimeout)
}

// concurrentStages 把多个子段合成一个段,并发执行其 run(整段内并行),emit 经互斥串行化以保证线程安全。
// 从编排器看仍是单个串行段:同段并行不破坏跨段(尤其稳定性段)独占。
func concurrentStages(name string, stages ...examStage) examStage {
	return examStage{
		name: name,
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			var mu sync.Mutex
			safeEmit := func(e ExamEvent) {
				mu.Lock()
				defer mu.Unlock()
				emit(e)
			}
			var wg sync.WaitGroup
			for _, s := range stages {
				wg.Add(1)
				go func(s examStage) {
					defer wg.Done()
					s.run(ctx, safeEmit, report)
				}(s)
			}
			wg.Wait()
		},
	}
}

// defaultEgressProbe 生产探测器:从 node 建一条 mihomo 会话(整段三类探测复用该 client 连接池),
// 各类经该会话请求对应端点。与解锁/多地域段同构:每场体检独立会话,测试可注入假探测器绕过真实网络。
func (d *Detector) defaultEgressProbe(node *subscription.Node) (EgressProbe, error) {
	adapter, err := NewProxyAdapter(node)
	if err != nil {
		return EgressProbe{}, err
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: adapter.DialContext},
		Timeout:   egressSegmentTimeout,
	}
	return EgressProbe{
		IPv4: func(ctx context.Context) EgressIPv4 {
			_, body, err := getProbe(ctx, client, egressIPv4URL)
			if err != nil {
				return EgressIPv4{Error: fmt.Sprintf("请求失败: %v", err)}
			}
			res, perr := parseIPv4Egress(body)
			if perr != nil {
				return EgressIPv4{Error: perr.Error()}
			}
			return res
		},
		IPv6: func(ctx context.Context) EgressIPv6 {
			_, body, err := getProbe(ctx, client, egressIPv6URL)
			return classifyIPv6Egress(body, err)
		},
		DNS: func(ctx context.Context) EgressDNS {
			_, body, err := getProbe(ctx, client, egressDNSURL)
			if err != nil {
				return EgressDNS{Error: fmt.Sprintf("请求失败: %v", err)}
			}
			res, perr := parseDNSEgress(body)
			if perr != nil {
				return EgressDNS{Error: perr.Error()}
			}
			return res
		},
	}, nil
}
