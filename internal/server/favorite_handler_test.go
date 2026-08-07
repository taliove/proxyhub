package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// issue #83:节点收藏 API(POST /api/nodes/{nodeKey}/favorite)+ 列表视图 favorite 标记。
// fixture 全合成(1.1.1.1 等字面量 + 假机场名),不触网。

// postNodeFavorite 调收藏 toggle,返回响应码
func postNodeFavorite(t *testing.T, h http.Handler, cookie *http.Cookie, nodeKey string, favorite bool) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"favorite": favorite})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+nodeKey+"/favorite", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

// listNodeFavorites 拉 /api/nodes,返回 node_key -> favorite 映射
func listNodeFavorites(t *testing.T, h http.Handler, cookie *http.Cookie) map[string]bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=100", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list nodes status = %d (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Nodes []struct {
			NodeKey  string `json:"node_key"`
			Favorite bool   `json:"favorite"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal nodes: %v", err)
	}
	out := make(map[string]bool, len(resp.Nodes))
	for _, n := range resp.Nodes {
		out[n.NodeKey] = n.Favorite
	}
	return out
}

func TestNodeFavoriteAPI_ToggleAndListMarking(t *testing.T) {
	srv, st := newTestServer(t, nodeMgmtNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 自建节点以库为权威(serve-time 合并),先落库与池同身份的自建行,
	// 否则列表里看不到"自建美国"(mergeSelfHosted 丢弃池内自建、按库重建)。
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建美国", Protocol: "trojan", Server: "3.3.3.3", Port: 443,
		Password: "p", TLS: true, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	// 机场节点与自建节点均可收藏
	if code := postNodeFavorite(t, h, cookie, "1.1.1.1:8388", true); code != http.StatusOK {
		t.Fatalf("favorite airport node status = %d, want 200", code)
	}
	if code := postNodeFavorite(t, h, cookie, "3.3.3.3:443", true); code != http.StatusOK {
		t.Fatalf("favorite self node status = %d, want 200", code)
	}

	favs := listNodeFavorites(t, h, cookie)
	if !favs["1.1.1.1:8388"] {
		t.Error("airport node favorite = false, want true")
	}
	if !favs["3.3.3.3:443"] {
		t.Error("self node favorite = false, want true")
	}
	if favs["2.2.2.2:8388"] {
		t.Error("untouched node favorite = true, want false")
	}

	// 取消收藏 -> 列表标记消失(跨请求持久于服务端,重新拉取仍正确)
	if code := postNodeFavorite(t, h, cookie, "1.1.1.1:8388", false); code != http.StatusOK {
		t.Fatalf("unfavorite status = %d, want 200", code)
	}
	favs = listNodeFavorites(t, h, cookie)
	if favs["1.1.1.1:8388"] {
		t.Error("airport node favorite still true after unfavorite")
	}
	if !favs["3.3.3.3:443"] {
		t.Error("self node favorite lost after unfavoriting another node")
	}
}

func TestNodeFavoriteAPI_BadRequest(t *testing.T) {
	srv, _ := newTestServer(t, nodeMgmtNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 缺 favorite 字段(必须显式布尔,避免零值误吞)
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/1.1.1.1:8388/favorite", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing favorite status = %d, want 400", w.Code)
	}

	// 非法 JSON
	req = httptest.NewRequest(http.MethodPost, "/api/nodes/1.1.1.1:8388/favorite", bytes.NewReader([]byte("{")))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid json status = %d, want 400", w.Code)
	}
}
