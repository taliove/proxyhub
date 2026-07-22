package speedtest

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewRandomBlock_Incompressible 随机块循环拼接成的大流必须经 gzip 不缩水
// (反例:bandwidth_stream.go durationReader 的递增序列会被压垮,测速数字虚高)。
func TestNewRandomBlock_Incompressible(t *testing.T) {
	block, err := NewRandomBlock(DownloadBlockSize)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	if len(block) != DownloadBlockSize {
		t.Fatalf("block size = %d, want %d", len(block), DownloadBlockSize)
	}

	// 循环拼接 8 倍块长,模拟下行发流的数据形态
	var raw bytes.Buffer
	for i := 0; i < 8; i++ {
		raw.Write(block)
	}

	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(raw.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	ratio := float64(compressed.Len()) / float64(raw.Len())
	if ratio < 0.95 {
		t.Errorf("gzip ratio = %.3f, want >= 0.95 (stream must be incompressible)", ratio)
	}
}

// TestNewRandomBlock_RandomContent 两块随机块不应相同(crypto/rand 而非固定种子)。
func TestNewRandomBlock_RandomContent(t *testing.T) {
	a, err := NewRandomBlock(DownloadBlockSize)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	b, err := NewRandomBlock(DownloadBlockSize)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("two random blocks are identical, want distinct crypto/rand output")
	}
}

type countingWriter struct {
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// TestStreamDownload_DurationBound 到 deadline 即停,不写超。
func TestStreamDownload_DurationBound(t *testing.T) {
	block, err := NewRandomBlock(64 * 1024)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	w := &countingWriter{}
	start := time.Now()
	n, err := StreamDownload(context.Background(), w, block, start.Add(150*time.Millisecond), DownloadMaxBytes)
	if err != nil {
		t.Fatalf("StreamDownload: %v", err)
	}
	elapsed := time.Since(start)
	if n <= 0 {
		t.Error("StreamDownload wrote 0 bytes before deadline")
	}
	if elapsed > 3*time.Second {
		t.Errorf("StreamDownload elapsed %v, want bounded near deadline", elapsed)
	}
}

// TestStreamDownload_ByteCap maxBytes 兜底优先于时长:上限到即停。
func TestStreamDownload_ByteCap(t *testing.T) {
	block, err := NewRandomBlock(64 * 1024)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	w := &countingWriter{}
	cap := int64(3 * 64 * 1024)
	n, err := StreamDownload(context.Background(), w, block, time.Now().Add(time.Hour), cap)
	if err != nil {
		t.Fatalf("StreamDownload: %v", err)
	}
	if n != cap {
		t.Errorf("StreamDownload bytes = %d, want exactly %d (byte cap)", n, cap)
	}
	if w.n != cap {
		t.Errorf("writer received %d bytes, want %d", w.n, cap)
	}
}

// TestStreamDownload_ContextCancel 客户端断开(ctx 取消)即收手,不空转。
func TestStreamDownload_ContextCancel(t *testing.T) {
	block, err := NewRandomBlock(64 * 1024)
	if err != nil {
		t.Fatalf("NewRandomBlock: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &countingWriter{}
	if _, err := StreamDownload(ctx, w, block, time.Now().Add(time.Hour), DownloadMaxBytes); err == nil {
		t.Error("StreamDownload with cancelled ctx: err = nil, want non-nil")
	}
}

// TestCountUpload_CountsAndCaps 计数准确;超 maxBytes 截断在上限处。
func TestCountUpload_CountsAndCaps(t *testing.T) {
	payload := strings.Repeat("x", 100_000)
	n, err := CountUpload(strings.NewReader(payload), MaxUploadBytes)
	if err != nil {
		t.Fatalf("CountUpload: %v", err)
	}
	if n != 100_000 {
		t.Errorf("CountUpload = %d, want 100000", n)
	}

	n, err = CountUpload(strings.NewReader(payload), 50_000)
	if err != nil {
		t.Fatalf("CountUpload capped: %v", err)
	}
	if n != 50_000 {
		t.Errorf("CountUpload capped = %d, want 50000", n)
	}
}

// TestCountUpload_Empty 空 body 计 0,不报错。
func TestCountUpload_Empty(t *testing.T) {
	n, err := CountUpload(strings.NewReader(""), MaxUploadBytes)
	if err != nil {
		t.Fatalf("CountUpload: %v", err)
	}
	if n != 0 {
		t.Errorf("CountUpload empty = %d, want 0", n)
	}
}

