package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// parseSSEFrames 从 SSE 响应体拆出每个 "data: {...}" 帧并解析为 map。
func parseSSEFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("parse frame %q: %v", raw, err)
		}
		frames = append(frames, m)
	}
	return frames
}

func TestHandleNodeExamStream_EventSequence(t *testing.T) {
	node := &subscription.Node{Name: "exam-node", Server: "example.com", Port: 443, Type: "vmess", Source: "airport"}
	srv, _ := newTestServer(t, []*subscription.Node{node})

	// 注入假探测器(不触真实网络)+ 短配置(3 次采样,间隔小),让 SSE 迅速跑完。
	det := srv.detectionService.detector
	det.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		lat := []int{40, 55, 48}
		i := 0
		return func(context.Context) (int, bool) {
			v := lat[i%len(lat)]
			i++
			return v, true
		}, nil
	})
	det.SetExamConfigProvider(func() detection.ExamConfig {
		return detection.ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 300, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 5}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	frames := parseSSEFrames(t, w.Body.String())
	if len(frames) < 3 {
		t.Fatalf("frames = %d, want >=3 (samples + section_done + done)", len(frames))
	}

	// 序列断言:先 N 个 sample,再 section_done,最后 done。
	var sampleCount, sectionDoneAt, doneAt = 0, -1, -1
	for i, f := range frames {
		switch f["phase"] {
		case "sample":
			if sectionDoneAt != -1 {
				t.Errorf("sample frame appeared after section_done (index %d)", i)
			}
			if f["section"] != "stability" {
				t.Errorf("sample[%d] section = %v, want stability", i, f["section"])
			}
			sampleCount++
		case "section_done":
			sectionDoneAt = i
			if _, ok := f["metrics"]; !ok {
				t.Errorf("section_done frame missing metrics")
			}
		case "done":
			doneAt = i
		}
	}

	if sampleCount != 3 {
		t.Errorf("sample frames = %d, want 3", sampleCount)
	}
	if sectionDoneAt == -1 || doneAt == -1 {
		t.Fatalf("missing section_done(%d) or done(%d)", sectionDoneAt, doneAt)
	}
	if !(sectionDoneAt < doneAt) {
		t.Errorf("order wrong: section_done at %d, done at %d (want section_done first)", sectionDoneAt, doneAt)
	}
	if doneAt != len(frames)-1 {
		t.Errorf("done at %d, want last frame (%d)", doneAt, len(frames)-1)
	}
}

func TestHandleNodeExamStream_NodeNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key=missing", nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
