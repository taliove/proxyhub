package region

import (
	"context"
	"log/slog"
	"net"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// NewFromStore 装配生产识别器(aggregator 全量刷新与 poolops 单机场 upsert
// 两处装配点共用):L1 名称规则表 + L2 emoji 反解(内置)+ L3 DoH/系统 DNS +
// 离线 GeoIP + node_server_geo 持久缓存。
//
// 识别是 best-effort,各层构造失败只降级不报错:L1 规则加载失败跳过 L1;
// 直连出口关闭或 DoH seam 构造失败回落系统 resolver(无 TUN 劫持面时系统
// resolver 即可信);缓存写失败仅 warn,不影响识别结果。
func NewFromStore(st *store.Store, logger *slog.Logger) *Recognizer {
	if logger == nil {
		logger = slog.Default()
	}
	deps := Deps{
		LookupCountry: geoip.LookupCountry,
		GetCached:     st.GetServerGeo,
		PutCached: func(host, code string) {
			if err := st.PutServerGeo(host, code); err != nil {
				logger.Warn("region: persist server geo cache failed", "host", host, "error", err)
			}
		},
	}

	if rec, err := st.NewRegionRecognizer(); err != nil {
		logger.Warn("region: load name rules failed, L1 disabled", "error", err)
	} else {
		deps.RecognizeName = rec.Recognize
	}

	cfg := st.GetDirectEgressConfig()
	if cfg.Enabled {
		if resolve, err := detection.NewHostResolver(cfg); err != nil {
			// DoH 端点配置非法等:回落系统 resolver,识别仍可用(无 TUN 时等价)。
			logger.Warn("region: build DoH resolver failed, fall back to system resolver", "error", err)
			deps.LookupHost = systemLookupHost
		} else {
			deps.LookupHost = resolve
		}
	} else {
		// 直连出口关闭 = 用户声明无 TUN 劫持面,系统 resolver 即可信(与检测链路同一开关)。
		deps.LookupHost = systemLookupHost
	}

	return New(deps)
}

// systemLookupHost 系统 resolver 的 LookupHost 适配(带 ctx)。
func systemLookupHost(ctx context.Context, host string) ([]string, error) {
	var r net.Resolver
	return r.LookupHost(ctx, host)
}
