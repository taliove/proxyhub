package detection

import (
	"net/http"
	"testing"
)

// TestClassifyClaude 表驱动:Claude 未支持地区(403 + 区域不可用标记)判 blocked;
// 正常 2xx 判 available;裸 403 / 5xx / 超时(交由 error 路径)判 inconclusive,不误判 blocked。
func TestClassifyClaude(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   unlockVerdict
	}{
		{"not available in region", http.StatusForbidden, `<html><body>Anthropic's services are not available in your region.</body></html>`, verdictBlocked},
		{"app unavailable", http.StatusForbidden, `{"type":"error","error":{"message":"app unavailable"}}`, verdictBlocked},
		{"ok landing", http.StatusOK, `<!doctype html><title>Claude</title>`, verdictAvailable},
		{"bare forbidden", http.StatusForbidden, "Forbidden", verdictInconclusive},
		{"service unavailable 5xx", http.StatusServiceUnavailable, "maintenance", verdictInconclusive},
	}
	for _, c := range cases {
		if got := classifyClaude(c.status, c.body); got != c.want {
			t.Errorf("%s: classifyClaude(%d) = %v, want %v", c.name, c.status, got, c.want)
		}
	}
}

// TestClaudeRegistered init 已把 Claude 判定器注册进注册表。
func TestClaudeRegistered(t *testing.T) {
	if _, ok := unlockCheckers[KindClaude]; !ok {
		t.Fatal("KindClaude checker not registered by init")
	}
}
