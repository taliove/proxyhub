package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// triggerPlaneFixtureNodes 两个用户各一个池节点(全零 UUID + example.com,绝无真实凭证)。
func triggerPlaneFixtureNodes() (admin, member *subscription.Node) {
	return &subscription.Node{
			Name: "admin-node", Server: "a.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", UserID: 1,
		}, &subscription.Node{
			Name: "member-node", Server: "b.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", UserID: 2,
		}
}

// TestTriggerPlane_BatchExamPerUserShard 批量体检按属主分片:
// 普通用户"全部节点"口径只覆盖自己池;落库历史按属主分桶,他人节点不被触碰,
// 且他人的体检历史对自己不可见(200 null 同态,不暴露存在性)。
func TestTriggerPlane_BatchExamPerUserShard(t *testing.T) {
	adminNode, memberNode := triggerPlaneFixtureNodes()
	srv, st := newTestServer(t, []*subscription.Node{adminNode, memberNode})
	injectFastExam(t, srv)
	member := UserScope{UserID: 2, Role: store.RoleUser}

	// member 以空 node_keys(scope=all)发起批量体检
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/exam/batch", strings.NewReader(`{}`))
	req = req.WithContext(ContextWithUserScope(req.Context(), member))
	srv.handleBatchExam(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", rec.Code, rec.Body.String())
	}
	waitJobTerminal(t, st, "batch_exam", "batch_exam")

	// member 节点的历史归属 user 2
	entry, err := st.LatestCompleteExamHistoryForUser(2, memberNode.NodeKey())
	if err != nil || entry == nil {
		t.Fatalf("member node: no exam history for user 2 (entry=%v, err=%v)", entry, err)
	}
	// admin 节点未被 member 的任务触碰(任何属主口径下都无新历史)
	if e, _ := st.LatestCompleteExamHistoryForUser(0, adminNode.NodeKey()); e != nil {
		t.Errorf("admin node examined by member's batch job: %+v", e)
	}
	// member 读 admin 节点的历史:不可见(null 同态)
	rec = httptest.NewRecorder()
	srv.handleGetExamLatest(rec, scopedRequest(http.MethodGet,
		"/api/nodes/exam/latest?node_key="+adminNode.NodeKey(), member))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Errorf("member reads admin exam history: code=%d body=%s, want 200 null", rec.Code, rec.Body.String())
	}
}

// TestTriggerPlane_SingleExamCrossUser404 单节点体检/测速/稳定性入口:
// 普通用户对他人池内节点发起一律 404(resolveTestNode 按属主限定)。
func TestTriggerPlane_SingleExamCrossUser404(t *testing.T) {
	adminNode, _ := triggerPlaneFixtureNodes()
	srv, _ := newTestServer(t, []*subscription.Node{adminNode})
	injectFastExam(t, srv)
	member := UserScope{UserID: 2, Role: store.RoleUser}

	paths := []string{
		"/api/nodes/exam/stream?node_key=" + adminNode.NodeKey(),
		"/api/nodes/stability/stream?node_key=" + adminNode.NodeKey(),
		"/api/nodes/test/stream?node_key=" + adminNode.NodeKey(),
		"/api/speedtest/proxy-latency?node_key=" + adminNode.NodeKey(),
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := scopedRequest(http.MethodGet, p, member)
			switch {
			case strings.Contains(p, "exam/stream"):
				srv.handleNodeExamStream(rec, req)
			case strings.Contains(p, "stability/stream"):
				srv.handleNodeStabilityStream(rec, req)
			case strings.Contains(p, "test/stream"):
				srv.handleTestNodeStream(rec, req)
			default:
				srv.handleSpeedtestProxyLatency(rec, req)
			}
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestTriggerPlane_SelfNodeByNodeKey 自建节点按 node_key 可解析(生产实测 bug:
// 测速页只带 node_key,而自建节点不入原始池、只经 serve-time 合并出现,
// 导致 proxy-latency 等对自建节点一律 404)。属主隔离不松:他人自建仍查不到。
func TestTriggerPlane_SelfNodeByNodeKey(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNodeForUser(1, &store.SelfHostedNode{
		Name: "自建", Protocol: "ss", Server: "self.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNodeForUser: %v", err)
	}

	n := srv.resolveTestNode(1, 0, "self.example.com:8388")
	if n == nil {
		t.Fatal("self-hosted node must resolve by node_key (merged pool)")
	}
	if n.Source != subscription.SourceSelfHosted {
		t.Errorf("Source = %q, want self-hosted", n.Source)
	}

	// 他人视角:查不到(404 语义,不暴露存在性)
	if n := srv.resolveTestNode(2, 0, "self.example.com:8388"); n != nil {
		t.Errorf("user 2 resolved user 1's self node: %+v", n)
	}
	// 不存在的 key:仍查不到
	if n := srv.resolveTestNode(1, 0, "gone.example.com:8388"); n != nil {
		t.Errorf("unknown key resolved: %+v", n)
	}
}
