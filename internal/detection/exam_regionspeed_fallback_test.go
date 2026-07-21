package detection

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClassifyRegionError 测试失败原因分类:HTTP 状态码/超时/死链/传输错误。
func TestClassifyRegionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType string
	}{
		{name: "403", err: fmt.Errorf("status 403"), wantType: "HTTP 403"},
		{name: "404", err: fmt.Errorf("status 404"), wantType: "HTTP 404"},
		{name: "500", err: fmt.Errorf("status 500"), wantType: "HTTP 500"},
		{name: "timeout", err: context.DeadlineExceeded, wantType: "timeout"},
		{name: "deadlink", err: errDeadLink, wantType: "deadlink"},
		{name: "generic", err: errBoom, wantType: "transport"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRegionError(tc.err)
			if !strings.Contains(got, tc.wantType) {
				t.Errorf("classifyRegionError(%v) = %q, want contains %q", tc.err, got, tc.wantType)
			}
		})
	}
}

// TestMeasureRegionSpeedWithFallback_403Fallback 测试 403 触发回退:首个点 403,应回退到备用点。
func TestMeasureRegionSpeedWithFallback_403Fallback(t *testing.T) {
	body := strings.Repeat("x", 600*1024) // > minValidDownloadBytes
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer ok.Close()

	client := &http.Client{}
	slice := 50 * time.Millisecond
	hard := 3 * time.Second
	region := Region{Code: "test", Name: "Test", URL: forbidden.URL}
	fallbackURLs := []string{forbidden.URL, ok.URL}

	res := measureRegionSpeedWithFallback(context.Background(), client, region, slice, hard, fallbackURLs)
	if res.Error != "" {
		t.Fatalf("403 should fallback to second URL, got error: %q", res.Error)
	}
	if res.DownMbps <= 0 {
		t.Errorf("fallback should succeed with speed, got %+v", res)
	}
}

// TestMeasureRegionSpeedWithFallback_DeadLinkFallback 测试死链触发回退:首个点内容不足,应回退。
func TestMeasureRegionSpeedWithFallback_DeadLinkFallback(t *testing.T) {
	smallBody := strings.Repeat("x", 100) // 远低于 minValidDownloadBytes
	deadLink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(smallBody))
	}))
	defer deadLink.Close()
	validBody := strings.Repeat("y", 600*1024) // > minValidDownloadBytes
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validBody))
	}))
	defer ok.Close()

	client := &http.Client{}
	slice := 50 * time.Millisecond
	hard := 3 * time.Second
	region := Region{Code: "test", Name: "Test", URL: deadLink.URL}
	fallbackURLs := []string{deadLink.URL, ok.URL}

	res := measureRegionSpeedWithFallback(context.Background(), client, region, slice, hard, fallbackURLs)
	if res.Error != "" {
		t.Fatalf("dead link should fallback to second URL, got error: %q", res.Error)
	}
	if res.DownMbps <= 0 {
		t.Errorf("fallback should succeed with speed, got %+v", res)
	}
}

// TestMeasureRegionSpeedWithFallback_AllFailed 测试全失败:所有点都失败,应返回具体 error。
func TestMeasureRegionSpeedWithFallback_AllFailed(t *testing.T) {
	forbidden1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden1.Close()
	forbidden2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer forbidden2.Close()

	client := &http.Client{}
	slice := 50 * time.Millisecond
	hard := 3 * time.Second
	region := Region{Code: "test", Name: "Test", URL: forbidden1.URL}
	fallbackURLs := []string{forbidden1.URL, forbidden2.URL}

	res := measureRegionSpeedWithFallback(context.Background(), client, region, slice, hard, fallbackURLs)
	if res.Error == "" {
		t.Error("all URLs failed, should return error")
	}
	if !strings.Contains(res.Error, "HTTP") {
		t.Errorf("error should contain HTTP status, got: %q", res.Error)
	}
}
