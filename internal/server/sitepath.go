package server

import (
	"net/http"
	"net/url"
	"strings"
)

// sitePathMiddleware 在配置了 Site Path 时强制管理面路径边界:
//
//   - /sub 与 /sub/*:直通(不剥前缀)——订阅端点是公开拉取入口(随机 path +
//     token,不可枚举),挂在根命名空间,订阅链接不再携带 Site Path(issue #74:
//     链接进客户端/日志会泄露管理面路径)。IP 过滤/限流在链内侧,直通不受影响。
//   - /<site-path>/ 下的请求:剥掉前缀后下放给 mux(管理 UI / API 照常;
//     旧形式 /<site-path>/sub/* 由此保持双挂兼容,历史链接不失效)
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
		// 订阅端点根命名空间直通(sub 在保留字清单内,与 Site Path 无冲突)
		if p == "/sub" || strings.HasPrefix(p, "/sub/") {
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

// rewriteIndexForSitePath rewrites the built index.html for prefix-aware
// serving. The SPA is built with a relative base ("./assets/...", see
// web/vite.config.ts), which resolves against the DOCUMENT URL and breaks on
// deep client routes — under a Site Path (/<site-path>/airports) and at the
// root too (/mfa/enroll). So this runs for EVERY index.html serve:
//
//   - relative href/src become absolute: "/<sitePath>/..." when a Site Path
//     is configured, plain "/..." at the root
//   - window.__PH_BASE__ is injected only when a Site Path is set (the SPA
//     runtime defaults to '' at the root)
//
// Lazy-loaded chunks and CSS preload deps are module-relative (import.meta
// resolution) and follow the entry module's location, so no JS rewriting is
// needed beyond index.html. sitePath is validated to [A-Za-z0-9_-] at init,
// so embedding it in the inline script is safe.
func rewriteIndexForSitePath(data []byte, sitePath string) []byte {
	base := ""
	html := string(data)
	if sitePath != "" {
		base = "/" + sitePath
		html = strings.Replace(html, "<head>",
			`<head><script>window.__PH_BASE__="`+base+`"</script>`, 1)
	}
	// 顺序敏感:先替换绝对形式(空前缀时是恒等操作),再替换相对形式。
	// 反过来会让相对形式产生的绝对输出被绝对形式二次命中,拼出双重前缀。
	html = strings.ReplaceAll(html, `href="/`, `href="`+base+`/`)
	html = strings.ReplaceAll(html, `src="/`, `src="`+base+`/`)
	html = strings.ReplaceAll(html, `href="./`, `href="`+base+`/`)
	html = strings.ReplaceAll(html, `src="./`, `src="`+base+`/`)
	return []byte(html)
}
