package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// DefaultAPIBase ip-api.com 免费接口（限频 45 次/分钟，本系统用量远低于此）
const DefaultAPIBase = "http://ip-api.com/json"

// Resolver IP 地理位置解析器
type Resolver struct {
	apiBase string
	client  *http.Client
	store   *store.Store
}

// NewResolver 创建解析器。apiBase 为空时使用默认接口。
func NewResolver(st *store.Store, apiBase string) *Resolver {
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	return &Resolver{
		apiBase: apiBase,
		client:  &http.Client{Timeout: 10 * time.Second},
		store:   st,
	}
}

// ResolveOnce 解析一批未解析的 IP，返回成功解析的数量
func (r *Resolver) ResolveOnce(ctx context.Context, batch int) (int, error) {
	ips, err := r.store.UnresolvedIPs(batch)
	if err != nil {
		return 0, fmt.Errorf("list unresolved ips: %w", err)
	}

	resolved := 0
	for _, ip := range ips {
		select {
		case <-ctx.Done():
			return resolved, ctx.Err()
		default:
		}

		geo, err := r.lookup(ctx, ip)
		if err != nil {
			// 单个失败不影响其他，写入空记录避免反复重试
			continue
		}
		if err := r.store.SaveGeo(*geo); err != nil {
			continue
		}
		resolved++
	}
	return resolved, nil
}

// Run 后台定时解析
func (r *Resolver) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ResolveOnce(ctx, 20)
		}
	}
}

// lookup 调用 API 查询单个 IP
func (r *Resolver) lookup(ctx context.Context, ip string) (*store.GeoInfo, error) {
	url := fmt.Sprintf("%s/%s?lang=zh-CN&fields=status,country,regionName,city,isp", r.apiBase, ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query geo api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geo api status %d", resp.StatusCode)
	}

	var result struct {
		Status     string `json:"status"`
		Country    string `json:"country"`
		RegionName string `json:"regionName"`
		City       string `json:"city"`
		ISP        string `json:"isp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode geo response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("geo lookup failed for %s", ip)
	}

	return &store.GeoInfo{
		IP:      ip,
		Country: result.Country,
		Region:  result.RegionName,
		City:    result.City,
		ISP:     result.ISP,
	}, nil
}

// LookupIP 同步查询单个 IP 的地理信息(不写库),供自建节点存时解析地区复用。
func (r *Resolver) LookupIP(ctx context.Context, ip string) (*store.GeoInfo, error) {
	return r.lookup(ctx, ip)
}
