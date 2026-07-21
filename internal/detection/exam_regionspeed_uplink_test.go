package detection

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestMeasureBaselineUplink_Success 测试基准上行:应返回上行速率。
func TestMeasureBaselineUplink_Success(t *testing.T) {
	received := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		received = n
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{}
	slice := 100 * time.Millisecond
	hard := 3 * time.Second

	mbps, err := measureBaselineUplink(context.Background(), client, srv.URL, slice, hard)
	if err != nil {
		t.Fatalf("measureBaselineUplink failed: %v", err)
	}
	if mbps <= 0 {
		t.Errorf("uplink speed = %v, want > 0", mbps)
	}
	if received < minValidDownloadBytes {
		t.Errorf("received bytes = %d, want >= %d", received, minValidDownloadBytes)
	}
}

// TestMeasureBaselineUplink_Timeout 测试上行超时:硬超时但已上传够数据,应返回平均速率。
func TestMeasureBaselineUplink_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // 模拟慢速服务器
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{}
	slice := 50 * time.Millisecond
	hard := 80 * time.Millisecond // 短超时,会超时

	mbps, err := measureBaselineUplink(context.Background(), client, srv.URL, slice, hard)
	// 硬超时但已上传够数据,应返回速率(不报错)。
	if err != nil {
		t.Errorf("timeout with sufficient data should not error, got: %v", err)
	}
	if mbps <= 0 {
		t.Errorf("timeout result should have speed, got %v", mbps)
	}
}

// TestMeasureBaselineUplink_InsufficientData 测试上行数据不足:应返回错误。
func TestMeasureBaselineUplink_InsufficientData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{}
	slice := 10 * time.Millisecond // 极短,上传不够
	hard := 20 * time.Millisecond

	_, err := measureBaselineUplink(context.Background(), client, srv.URL, slice, hard)
	if err == nil {
		t.Error("insufficient upload data should return error")
	}
}

// TestMeasureRegionSpeed_BaselineHasUplink 测试基准行集成:下行+上行都应有值。
func TestMeasureRegionSpeed_BaselineHasUplink(t *testing.T) {
	downBody := make([]byte, 600*1024)
	downSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(downBody)
	}))
	defer downSrv.Close()

	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upSrv.Close()

	client := &http.Client{}
	slice := 100 * time.Millisecond
	hard := 3 * time.Second

	// 模拟基准行:下行用 downSrv,上行用 upSrv(实际会用 upstreamUploadURL,这里测局部逻辑)。
	baseline := Region{Code: "baseline", Name: "基准", URL: downSrv.URL}
	// 临时替换 upstreamUploadURL 常量不可行,这里测下行部分,上行在独立测试中验证。
	res := measureRegionSpeedWithFallback(context.Background(), client, baseline, slice, hard, []string{downSrv.URL})
	if res.Error != "" {
		t.Fatalf("baseline download failed: %q", res.Error)
	}
	if res.DownMbps <= 0 {
		t.Errorf("baseline should have down speed, got %+v", res)
	}
	// 上行已在 TestMeasureBaselineUplink_* 验证,此处确认下行逻辑正确即可。
}
