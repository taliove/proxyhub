package detection

import (
	"net/http"
	"testing"
)

// TestClassifyGemini 表驱动:Gemini 未支持地区(403 + 区域不可用标记)判 blocked;
// 正常 2xx 判 available;裸 403 / 5xx(交由 error 路径)判 inconclusive,不误判 blocked。
func TestClassifyGemini(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   unlockVerdict
	}{
		{"not available in country", http.StatusForbidden, `<html>Gemini isn't currently available in your country</html>`, verdictBlocked},
		{"not currently supported", http.StatusForbidden, `Gemini Apps aren't currently supported in your country`, verdictBlocked},
		{"ok landing", http.StatusOK, `<!doctype html><title>Gemini</title>`, verdictAvailable},
		{"bare forbidden", http.StatusForbidden, "403. That's an error.", verdictInconclusive},
		{"internal error 5xx", http.StatusInternalServerError, "500 error", verdictInconclusive},
	}
	for _, c := range cases {
		if got := classifyGemini(c.status, c.body); got != c.want {
			t.Errorf("%s: classifyGemini(%d) = %v, want %v", c.name, c.status, got, c.want)
		}
	}
}

// TestGeminiRegistered init 已把 Gemini 判定器注册进注册表。
func TestGeminiRegistered(t *testing.T) {
	if _, ok := unlockCheckers[KindGemini]; !ok {
		t.Fatal("KindGemini checker not registered by init")
	}
}
