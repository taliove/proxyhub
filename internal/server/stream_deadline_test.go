package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// deadlineRecorder 记录 SetWriteDeadline 调用的测试 writer。
// httptest.ResponseRecorder 不支持 deadline(ResponseController 走 ErrNotSupported 分支),
// 观察"端点自设写 deadline"需要底层 writer 实现 SetWriteDeadline。
type deadlineRecorder struct {
	header      http.Header
	body        bytes.Buffer
	code        int
	deadline    time.Time
	deadlineSet bool
}

func newDeadlineRecorder() *deadlineRecorder {
	return &deadlineRecorder{header: http.Header{}}
}

func (r *deadlineRecorder) Header() http.Header { return r.header }

func (r *deadlineRecorder) Write(p []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *deadlineRecorder) WriteHeader(code int) { r.code = code }

func (r *deadlineRecorder) Flush() {}

func (r *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	r.deadline = t
	r.deadlineSet = true
	return nil
}

// assertDeadlineNear 断言自设写 deadline 落在 budget 附近(±2s 容差)。
func assertDeadlineNear(t *testing.T, rec *deadlineRecorder, budget time.Duration) {
	t.Helper()
	if !rec.deadlineSet {
		t.Fatal("handler did not set a write deadline")
	}
	got := time.Until(rec.deadline)
	if got < budget-2*time.Second || got > budget+2*time.Second {
		t.Errorf("write deadline budget = %v, want ~%v", got.Round(time.Millisecond), budget)
	}
}

// TestStreamDeadline_DirectDownload 直发流端点自设写 deadline = 发流时长 + 收尾余量,
// 且全程流完(无全局 WriteTimeout 截断)。
func TestStreamDeadline_DirectDownload(t *testing.T) {
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := newDeadlineRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/download?duration_ms=1000", nil)

	s.handleSpeedtestDownload(rec, req)

	if rec.code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.code)
	}
	assertDeadlineNear(t, rec, time.Second+streamWriteSlack)
	if rec.body.Len() == 0 {
		t.Error("streamed body is empty, want non-zero bytes")
	}
}

// TestStreamDeadline_PassthroughDownload 透传下载端点自设写 deadline
// = proxySpeedtestMaxDuration + 收尾余量;即使代理客户端构造失败(错误路径)
// deadline 也已落上(慢连接边界先于业务失败建立)。
func TestStreamDeadline_PassthroughDownload(t *testing.T) {
	markerNode := &subscription.Node{
		Name: "marker", Type: "SECRET-MARKER", Server: "example.com", Port: 1234,
	}
	srv, _ := newTestServer(t, []*subscription.Node{markerNode})
	rec := newDeadlineRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/speedtest/proxy-download/stream?node_key="+markerNode.NodeKey(), nil)

	srv.handleSpeedtestProxyDownload(rec, req)

	if rec.code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.code, rec.body.String())
	}
	assertDeadlineNear(t, rec, proxySpeedtestMaxDuration+streamWriteSlack)
}

// TestStreamDeadline_PassthroughUpload 透传上传端点同样自设写 deadline。
func TestStreamDeadline_PassthroughUpload(t *testing.T) {
	markerNode := &subscription.Node{
		Name: "marker", Type: "SECRET-MARKER", Server: "example.com", Port: 1234,
	}
	srv, _ := newTestServer(t, []*subscription.Node{markerNode})
	rec := newDeadlineRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/speedtest/proxy-upload/stream?node_key="+markerNode.NodeKey(),
		strings.NewReader("x"))

	srv.handleSpeedtestProxyUpload(rec, req)

	if rec.code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.code, rec.body.String())
	}
	assertDeadlineNear(t, rec, proxySpeedtestMaxDuration+streamWriteSlack)
}

// TestStreamDeadline_TestNodeStream SSE 流式带宽测试端点自设写 deadline,
// 与流式测速墙钟预算对齐(默认 2×20s 单方向硬超时 + 15s 收尾余量 = 55s);
// 死节点快速收口,done 帧照常到达。
func TestStreamDeadline_TestNodeStream(t *testing.T) {
	node := &subscription.Node{
		Name: "dead-node", Server: "127.0.0.1", Port: 1, Type: "vless",
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport",
	}
	srv, _ := newTestServer(t, []*subscription.Node{node})
	rec := newDeadlineRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/test/stream?node_key="+node.NodeKey(), nil)

	srv.handleTestNodeStream(rec, req)

	if rec.code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.code, rec.body.String())
	}
	// 默认带宽配置:TestDurationSec=10、DirTimeoutSec=20 → 2*20s + 15s
	assertDeadlineNear(t, rec, 55*time.Second)
	if !strings.Contains(rec.body.String(), `"phase":"done"`) {
		t.Errorf("SSE body missing done frame: %q", rec.body.String())
	}
}

// TestSpeedtestDownload_WriteTimeoutExemption issue #158:直发流端点自设的写 deadline
// 覆盖任何全局 WriteTimeout(此处 100ms 缩短替身)——全局配置不再能截断流式响应;
// WriteTimeout=0(main.go 修复后形态)下同一流同样完整跑完。
// 客户端限速读取,控制测试流量体量。
func TestSpeedtestDownload_WriteTimeoutExemption(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 限速读取:每 4KB 睡 1ms,防止本地回环把 1.5s 发流灌成 GB 级体量。
	readThrottled := func(ts *httptest.Server) (int64, time.Duration, error) {
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/speedtest/download?duration_ms=1500", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, time.Since(start), err
		}
		defer resp.Body.Close()
		buf := make([]byte, 4096)
		var total int64
		for {
			n, rerr := resp.Body.Read(buf)
			total += int64(n)
			if rerr != nil {
				if rerr == io.EOF {
					rerr = nil
				}
				return total, time.Since(start), rerr
			}
			time.Sleep(time.Millisecond)
		}
	}

	assertFullStream := func(t *testing.T, ts *httptest.Server) {
		t.Helper()
		total, elapsed, err := readThrottled(ts)
		if err != nil {
			t.Fatalf("stream cut: %v (%d bytes in %v)", err, total, elapsed)
		}
		if elapsed < 1500*time.Millisecond {
			t.Errorf("stream lasted %v, want full 1.5s duration", elapsed)
		}
		if total == 0 {
			t.Error("streamed body is empty, want non-zero bytes")
		}
	}

	t.Run("endpoint deadline overrides global write timeout", func(t *testing.T) {
		ts := httptest.NewUnstartedServer(h)
		ts.Config.WriteTimeout = 100 * time.Millisecond // 旧全局 30s 的缩短替身
		ts.Start()
		defer ts.Close()
		assertFullStream(t, ts)
	})

	t.Run("zero write timeout lets stream finish", func(t *testing.T) {
		ts := httptest.NewUnstartedServer(h)
		ts.Config.WriteTimeout = 0 // 与 main.go 修复后一致
		ts.Start()
		defer ts.Close()
		assertFullStream(t, ts)
	})
}
