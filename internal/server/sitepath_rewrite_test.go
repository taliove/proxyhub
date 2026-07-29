package server

// rewriteIndexForSitePath 测试:base 注入 + 根绝对引用重写 + 幂等边界。
import (
	"os"
	"strings"
	"testing"
)

const spaTestIndex = `<!doctype html>
<html><head>
<link rel="icon" href="/proxyhub-icon.png" />
<script>try { localStorage.getItem('ph:dark') } catch (e) {}</script>
<script type="module" crossorigin src="/assets/index-abc123.js"></script>
<link rel="stylesheet" crossorigin href="/assets/index-def456.css">
</head><body><div id="app"></div></body></html>`

func TestRewriteIndexForSitePath(t *testing.T) {
	out := string(rewriteIndexForSitePath([]byte(spaTestIndex), "GTsRiXWBKs7El92a1HJ9"))

	for _, want := range []string{
		`window.__PH_BASE__="/GTsRiXWBKs7El92a1HJ9"`,
		`src="/GTsRiXWBKs7El92a1HJ9/assets/index-abc123.js"`,
		`href="/GTsRiXWBKs7El92a1HJ9/assets/index-def456.css"`,
		`href="/GTsRiXWBKs7El92a1HJ9/proxyhub-icon.png"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten index missing %q", want)
		}
	}
	// 不得残留根绝对资源引用
	for _, bad := range []string{`src="/assets/`, `href="/assets/`, `href="/proxyhub-icon.png"`} {
		if strings.Contains(out, bad) {
			t.Errorf("rewritten index still contains root-absolute %q", bad)
		}
	}
}

func TestRewriteIndexForSitePath_EmptyPath(t *testing.T) {
	// 空 site path 不应被调用方走到,但防御一下:不产生 panic,内容不变形
	out := rewriteIndexForSitePath([]byte(spaTestIndex), "")
	if !strings.Contains(string(out), `src="/assets/index-abc123.js"`) {
		t.Error("empty site path should keep root-absolute references as-is")
	}
}

// TestRewriteIndexForSitePath_BuiltTemplate 锁住真实构建产物的模板形态:
// <head> 锚点与根绝对引用必须存在于 cmd/server/web/index.html,否则重写
// 会静默失效(资产 200 但 base 未注入,API/路由全 404,排障困难)。
func TestRewriteIndexForSitePath_BuiltTemplate(t *testing.T) {
	data, err := os.ReadFile("../../cmd/server/web/index.html")
	if err != nil {
		t.Skipf("frontend not built in this checkout: %v", err)
	}
	out := string(rewriteIndexForSitePath(data, "GTsRiXWBKs7El92a1HJ9"))
	if !strings.Contains(out, `window.__PH_BASE__="/GTsRiXWBKs7El92a1HJ9"`) {
		t.Error("built index.html: __PH_BASE__ injection missing (<head> anchor drift?)")
	}
	if !strings.Contains(out, `"/GTsRiXWBKs7El92a1HJ9/assets/`) {
		t.Error("built index.html: asset references not rewritten")
	}
}
