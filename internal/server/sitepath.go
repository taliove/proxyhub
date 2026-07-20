package server

import (
	"net/http"
	"net/url"
	"strings"
)

// distPrefix 是流量分发数据面的命名空间前缀,独立于 Site Path 边界存在,
// 不受管理面前缀约束(见 production-release 02:do not break distribution)。
const distPrefix = "/dist"

// sitePathMiddleware 在配置了 Site Path 时强制管理面路径边界:
//
//   - /<site-path>/ 下的请求:剥掉前缀后下放给 mux(管理 UI / API / 订阅照常工作)
//   - /dist 及其子路径:分发数据面原样放行,不剥前缀
//   - /、缺前缀、前缀之外的请求:一律返回普通 404(不暴露服务存在)
//
// 未配置 Site Path(开发/CI)时完全透传,行为与现状一致。
// 配置读取失败时按 404 处理(安全边界宁可全拒,也不退回无前缀暴露)。
func (s *Server) sitePathMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sitePath, err := s.st.GetSitePath()
		if err != nil {
			s.logger.Warn("read site path failed, rejecting request", "error", err)
			http.NotFound(w, r)
			return
		}
		if sitePath == "" {
			next.ServeHTTP(w, r)
			return
		}

		p := r.URL.Path

		// 分发数据面保留自己的命名空间前缀
		if p == distPrefix || strings.HasPrefix(p, distPrefix+"/") {
			next.ServeHTTP(w, r)
			return
		}

		prefix := "/" + sitePath
		var stripped string
		switch {
		case p == prefix:
			stripped = "/"
		case strings.HasPrefix(p, prefix+"/"):
			stripped = strings.TrimPrefix(p, prefix)
		default:
			http.NotFound(w, r)
			return
		}

		// 不修改原请求:浅拷贝请求与 URL 后仅改写路径(同 http.StripPrefix 做法)
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = stripped
		r2.URL.RawPath = ""
		next.ServeHTTP(w, r2)
	})
}
