package detection

import (
	"net/http"
	"testing"
)

// TestClassifyOpenAI 表驱动:OpenAI 未支持地区(403 + unsupported_country)判 blocked;
// 正常 2xx 判 available;裸 403 / 5xx / 429 判 inconclusive(交由 error 路径,不误判 blocked)。
func TestClassifyOpenAI(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   unlockVerdict
	}{
		{"unsupported country", http.StatusForbidden, `{"error":{"code":"unsupported_country","message":"Country, region, or territory not supported"}}`, verdictBlocked},
		{"ok cookie config", http.StatusOK, `{"cookie_config":{"consent_required":false}}`, verdictAvailable},
		{"bare forbidden", http.StatusForbidden, "Forbidden", verdictInconclusive},
		{"gateway error", http.StatusBadGateway, "<html>502</html>", verdictInconclusive},
		{"rate limited", http.StatusTooManyRequests, "Too Many Requests", verdictInconclusive},
	}
	for _, c := range cases {
		if got := classifyOpenAI(c.status, c.body); got != c.want {
			t.Errorf("%s: classifyOpenAI(%d) = %v, want %v", c.name, c.status, got, c.want)
		}
	}
}

// TestOpenAIRegistered init 已把 OpenAI 判定器注册进注册表。
func TestOpenAIRegistered(t *testing.T) {
	if _, ok := unlockCheckers[KindOpenAI]; !ok {
		t.Fatal("KindOpenAI checker not registered by init")
	}
}
