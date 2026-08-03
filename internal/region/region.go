// Package region 统一的节点地区识别器(issue #37):三条入池路径(全量刷新 /
// 单机场 upsert / 手动导入)共用的唯一识别入口,三层短路兜底——
//
//	L1 名称规则表(组合 store.RegionRecognizer,语义不变,机场标注最准优先)
//	L2 国旗 emoji 反解(subscription.RegionCodeFromEmoji,零网络零枚举表)
//	L3 GeoIP(server 是 IP 直用;域名先查持久缓存,未命中才 DNS 解析再离线查库)
//
// L3 是 best-effort:任何失败(DNS 不通/无记录/私网)静默降级为 Unknown,
// 不复用检测链路的严格模式语义;负缓存 + 有界并发 + 短超时防止全量刷新
// 被逐节点 DNS 拖慢(计划点名的最易翻车点)。
package region

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Unknown 三层全部未命中时的兜底地区码(与 store.RegionRecognizer 口径一致)。
const Unknown = "Unknown"

// L3 成本纪律:短超时 + 有界并发 + 负缓存,防 177+ 节点逐轮 DNS 把刷新拖到分钟级。
const (
	// lookupTimeout 单 host DNS 解析的超时上限(DoH 客户端自身另有 5s 上限,取更紧者)。
	lookupTimeout = 3 * time.Second
	// batchConcurrency 批量识别时 L3 DNS 的并发上限。
	batchConcurrency = 16
	// cachePositiveTTL 正缓存(解析出国家)有效期。
	cachePositiveTTL = 7 * 24 * time.Hour
	// cacheNegativeTTL 负缓存(DNS 失败/无记录)有效期:短 TTL 防每轮重试,又不永久封死恢复。
	cacheNegativeTTL = time.Hour
)

// Request 单个节点的识别输入。
type Request struct {
	Name   string // 节点名(L1/L2 输入)
	Server string // 节点地址:IP 或域名(L3 输入)
}

// Deps 识别器外部依赖,函数注入(仿 server 包 regionResolverDeps 三函数模式,
// 单测以计数假 deps 断言层间短路)。逐层 nil 容忍:nil 即跳过该层,识别降级不报错。
type Deps struct {
	// RecognizeName L1 名称规则表(store.RegionRecognizer.Recognize):
	// 命中返回地区码,未命中返回 "Unknown"(或空串)。nil 跳过 L1。
	RecognizeName func(name string) string
	// LookupHost L3 DNS 解析(DoH seam 或系统 resolver)。nil 时域名不进 L3
	// (IP 字面量仍直接走离线 GeoIP,零网络)。
	LookupHost func(ctx context.Context, host string) ([]string, error)
	// LookupCountry L3 离线 IP -> ISO 3166-1 alpha-2(geoip.LookupCountry)。nil 跳过 L3。
	LookupCountry func(ip string) (string, error)
	// GetCached L3 持久缓存读(node_server_geo):ok=false 表示无记录。
	// code 空串 = 负缓存行。新鲜度(TTL)由本包判定。nil 不读缓存。
	GetCached func(host string) (code string, resolvedAt time.Time, ok bool)
	// PutCached L3 持久缓存写,code 空串 = 写负缓存。nil 不写缓存。
	PutCached func(host, code string)
	// Now 时钟,测试注入;nil 用 time.Now。
	Now func() time.Time
}

// Recognizer 三层识别器,并发安全(deps 皆为无副作用函数或内部带锁的实现)。
type Recognizer struct {
	deps Deps
}

// New 以注入依赖构造识别器。
func New(deps Deps) *Recognizer {
	return &Recognizer{deps: deps}
}

// Recognize 识别单个节点的地区码,三层短路,全未命中返回 Unknown。
func (r *Recognizer) Recognize(ctx context.Context, name, server string) string {
	if code := r.nameLayers(name); code != "" {
		return code
	}
	if code := r.geoFallback(ctx, server); code != "" {
		return code
	}
	return Unknown
}

// RecognizeBatch 批量识别,返回与 reqs 等长同序的地区码切片。
// L1/L2 逐条内联(零 I/O);进入 L3 的节点按 server 去重后有界并发解析,
// 同一 host 多节点只发一次 DNS(ctx 取消时进行中的解析随 ctx 中断,未启动的不再启动)。
func (r *Recognizer) RecognizeBatch(ctx context.Context, reqs []Request) []string {
	out := make([]string, len(reqs))

	// L1+L2:纯 CPU,逐条短路;需要 L3 的按 server 归组(去重 DNS)
	hostIdx := make(map[string][]int)
	var hosts []string
	for i, q := range reqs {
		if code := r.nameLayers(q.Name); code != "" {
			out[i] = code
			continue
		}
		if _, seen := hostIdx[q.Server]; !seen {
			hosts = append(hosts, q.Server)
		}
		hostIdx[q.Server] = append(hostIdx[q.Server], i)
	}

	if len(hosts) > 0 {
		results := make(map[string]string, len(hosts))
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, batchConcurrency)
		for _, h := range hosts {
			wg.Add(1)
			go func(h string) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				code := r.geoFallback(ctx, h)
				mu.Lock()
				results[h] = code
				mu.Unlock()
			}(h)
		}
		wg.Wait()
		for h, idxs := range hostIdx {
			code := results[h]
			if code == "" {
				code = Unknown
			}
			for _, i := range idxs {
				out[i] = code
			}
		}
	}
	return out
}

// nameLayers L1 名称规则表 -> L2 国旗 emoji 反解,命中返回地区码,否则空串。
func (r *Recognizer) nameLayers(name string) string {
	if r.deps.RecognizeName != nil {
		if code := r.deps.RecognizeName(name); code != "" && code != Unknown {
			return code
		}
	}
	return subscription.RegionCodeFromEmoji(name)
}

// geoFallback L3 GeoIP 链:server 是 IP 直用(零网络);域名先查持久缓存,
// 未命中才 DNS 解析取首个 IP 再离线查库。任何失败返回空串(调用方兜底 Unknown),
// 域名维度失败写负缓存。IP 字面量不入缓存(离线查询本身零成本)。
func (r *Recognizer) geoFallback(ctx context.Context, server string) string {
	if server == "" || r.deps.LookupCountry == nil {
		return ""
	}
	if net.ParseIP(server) != nil {
		code, err := r.deps.LookupCountry(server)
		if err != nil {
			return "" // 私网/保留段无记录,静默降级
		}
		return code
	}

	// 域名:缓存优先(含负缓存)
	if code, ok := r.cachedCode(server); ok {
		return code
	}
	if r.deps.LookupHost == nil {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	ips, err := r.deps.LookupHost(lookupCtx, server)
	cancel()
	if err != nil || len(ips) == 0 {
		// 仅真实 DNS 失败(父 ctx 存活,含 lookupTimeout 超时)写负缓存;
		// 本地取消(父 ctx 已取消)不是对端事实,不污染缓存。
		if ctx.Err() == nil {
			r.putCache(server, "") // DNS 失败:负缓存防每轮重试
		}
		return ""
	}
	code, err := r.deps.LookupCountry(ips[0])
	if err != nil || code == "" {
		r.putCache(server, "") // 解析到私网/无记录:同样负缓存
		return ""
	}
	r.putCache(server, code)
	return code
}

// cachedCode 读持久缓存并按 TTL 判定新鲜度:正缓存 7 天,负缓存 1 小时。
// 过期视同未命中(调用方重解析后覆盖)。
func (r *Recognizer) cachedCode(host string) (string, bool) {
	if r.deps.GetCached == nil {
		return "", false
	}
	code, resolvedAt, ok := r.deps.GetCached(host)
	if !ok {
		return "", false
	}
	now := time.Now()
	if r.deps.Now != nil {
		now = r.deps.Now()
	}
	ttl := cachePositiveTTL
	if code == "" {
		ttl = cacheNegativeTTL
	}
	if now.Sub(resolvedAt) > ttl {
		return "", false
	}
	return code, true
}

func (r *Recognizer) putCache(host, code string) {
	if r.deps.PutCached != nil {
		r.deps.PutCached(host, code)
	}
}
