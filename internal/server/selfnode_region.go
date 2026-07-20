package server

import (
	"context"
	"net"
)

// regionResolverDeps 存时地区解析的外部依赖(便于单测注入)。
type regionResolverDeps struct {
	lookupHost func(host string) ([]string, error)                 // DNS 解析,通常 net.LookupHost
	geoLookup  func(ctx context.Context, ip string) (string, error) // IP → 国家中文名
	recognize  func(name string) string                             // 名称/国家名 → 地区码,未命中返回 "Unknown"
}

// resolveRegionCode 解析自建节点的真实地区码。
// 顺序:server 是 IP 直接用,否则 DNS 取首个 IP → GeoIP 得国家名 → 识别器转地区码;
// GeoIP 链失败则回退按节点名识别;仍失败返回 "Unknown"(交给命名模板兜底)。
func resolveRegionCode(ctx context.Context, server, nodeName string, d regionResolverDeps) string {
	ip := ""
	if net.ParseIP(server) != nil {
		ip = server
	} else if addrs, err := d.lookupHost(server); err == nil && len(addrs) > 0 {
		ip = addrs[0]
	}

	if ip != "" {
		if country, err := d.geoLookup(ctx, ip); err == nil && country != "" {
			if code := d.recognize(country); code != "Unknown" {
				return code
			}
		}
	}

	if code := d.recognize(nodeName); code != "Unknown" {
		return code
	}
	return "Unknown"
}
