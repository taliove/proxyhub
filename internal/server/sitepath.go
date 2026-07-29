package server

import (
	"net/http"
	"net/url"
	"strings"
)

// sitePathMiddleware 在配置了 Site Path 时强制管理面路径边界:
//
//   - /<site-path>/ 下的请求:剥掉前缀后下放给 mux(管理 UI / API / 订阅照常工作)
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

// rewriteIndexForSitePath rewrites the built index.html for a Site Path
// deployment. The SPA is built with root-absolute references ("/assets/...",
// "/proxyhub-icon.png"), which bypass the /<site-path>/ prefix and 404 at
// both the reverse proxy and sitePathMiddleware. Two rewrites:
//
//   - inject window.__PH_BASE__ so the SPA runtime (router history, API
//     baseURL, subscription URL builders) generates prefixed URLs
//   - rewrite root-absolute href/src to live under the prefix
//
// sitePath is validated to [A-Za-z0-9_-] at init, so embedding it in the
// inline script is safe. Dynamic-import chunks resolve relative to the entry
// module URL (import.meta.url), so no JS rewriting is needed beyond index.html.
func rewriteIndexForSitePath(data []byte, sitePath string) []byte {
	if sitePath == "" {
		return data
	}
	base := "/" + sitePath
	html := string(data)
	html = strings.Replace(html, "<head>",
		`<head><script>window.__PH_BASE__="`+base+`"</script>`, 1)
	html = strings.ReplaceAll(html, `href="/`, `href="`+base+`/`)
	html = strings.ReplaceAll(html, `src="/`, `src="`+base+`/`)
	return []byte(html)
}
