package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// 自建节点身份查重(issue #53):创建/编辑时同 server/port/protocol 拒绝,
// 从源头杜绝重复行(重复行导致展示重复、删除体验断裂)。fixture 全合成。
func TestSelfNode_DedupeOnCreateAndUpdate(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.countryLookup = func(string) (string, error) { return "HK", nil }

	const nodeA = `{"name":"A","protocol":"vless","server":"dup.example.com","port":443,"uuid":"00000000-0000-0000-0000-000000000000"}`

	// 首次创建成功
	if w := createSelfNode(t, srv, nodeA); w.Code != http.StatusOK {
		t.Fatalf("首次创建 status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	// 同身份(同 server/port/protocol,非身份字段 password 不同)拒绝 409
	w := createSelfNode(t, srv, `{"name":"A-copy","protocol":"vless","server":"dup.example.com","port":443,"uuid":"00000000-0000-0000-0000-000000000000","password":"other-password"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("重复创建 status = %d, want 409, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "已存在") {
		t.Errorf("409 响应应说明重复原因, body = %s", w.Body.String())
	}

	// 不同端口不撞车
	if w := createSelfNode(t, srv, `{"name":"B","protocol":"vless","server":"dup.example.com","port":8443,"uuid":"00000000-0000-0000-0000-000000000000"}`); w.Code != http.StatusOK {
		t.Fatalf("不同端口创建 status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	// 拿到两行 id
	nodes, err := srv.st.ListAllSelfHostedNodesByUser(0)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("list: n=%d err=%v, want 2 行", len(nodes), err)
	}
	var idA, idB int64
	for _, n := range nodes {
		if n.Port == 443 {
			idA = n.ID
		} else {
			idB = n.ID
		}
	}

	update := func(id int64, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/self-nodes/"+strconv.FormatInt(id, 10), strings.NewReader(body))
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		w := httptest.NewRecorder()
		srv.handleUpdateSelfNode(w, req)
		return w
	}

	// 编辑 B 改成 A 的身份 → 409
	if w := update(idB, nodeA); w.Code != http.StatusConflict {
		t.Fatalf("编辑撞身份 status = %d, want 409, body = %s", w.Code, w.Body.String())
	}

	// 编辑 A 只改名(身份不变,排除自身)→ 200
	if w := update(idA, `{"name":"A-renamed","protocol":"vless","server":"dup.example.com","port":443,"uuid":"00000000-0000-0000-0000-000000000000"}`); w.Code != http.StatusOK {
		t.Fatalf("只改名 status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	// 改名后名称落库
	updated, err := srv.st.ListAllSelfHostedNodesByUser(0)
	if err != nil {
		t.Fatalf("list after update: %v", err)
	}
	for _, n := range updated {
		if n.ID == idA && n.Name != "A-renamed" {
			t.Errorf("name = %q, want A-renamed", n.Name)
		}
	}
}

// 响应体含用户可读的重复原因(前端 apiErrorMessage 直接展示纯文本)。
func TestSelfNode_DedupeMessageReadable(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.countryLookup = func(string) (string, error) { return "HK", nil }

	const body = `{"name":"A","protocol":"ss","server":"dup2.example.com","port":8388,"cipher":"aes-256-gcm","password":"ss-password"}`
	if w := createSelfNode(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("首次创建 status = %d", w.Code)
	}
	w := createSelfNode(t, srv, body)
	if w.Code != http.StatusConflict {
		t.Fatalf("重复创建 status = %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "相同服务器/端口/协议") {
		t.Errorf("响应未含重复原因, body = %s", w.Body.String())
	}
}
