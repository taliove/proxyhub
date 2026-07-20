package detection

import (
	"io"
	"testing"
	"time"
)

// slowReader 每次 Read 返回固定字节并 sleep,模拟受限带宽,用于验证采样节奏。
type slowReader struct {
	remaining int
	chunk     int
	delay     time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	n := s.chunk
	if n > len(p) {
		n = len(p)
	}
	if n > s.remaining {
		n = s.remaining
	}
	s.remaining -= n
	return n, nil
}

// TestSampleReader_EmitsSamples 验证 sampleReader 按间隔回调多次瞬时速率
func TestSampleReader_EmitsSamples(t *testing.T) {
	// 1MB,每 64KB 一读、每读 sleep 50ms → 约 16 读、800ms,应产生多个采样(间隔 300ms)
	src := &slowReader{remaining: 1024 * 1024, chunk: 64 * 1024, delay: 50 * time.Millisecond}

	var samples []Sample
	start := time.Now()
	sr := newSampleReader(src, "download", start, func(s Sample) {
		samples = append(samples, s)
	})

	if _, err := io.Copy(io.Discard, sr); err != nil {
		t.Fatalf("copy error = %v", err)
	}

	if len(samples) < 2 {
		t.Fatalf("采样点 = %d, want >= 2(应按 300ms 间隔多次采样)", len(samples))
	}
	for i, s := range samples {
		if s.Phase != "download" {
			t.Errorf("sample[%d].Phase = %q, want download", i, s.Phase)
		}
		if s.Mbps <= 0 {
			t.Errorf("sample[%d].Mbps = %.2f, want > 0", i, s.Mbps)
		}
		if s.ElapsedMs <= 0 {
			t.Errorf("sample[%d].ElapsedMs = %d, want > 0", i, s.ElapsedMs)
		}
	}
	if sr.TotalBytes() != 1024*1024 {
		t.Errorf("TotalBytes = %d, want %d", sr.TotalBytes(), 1024*1024)
	}
}

// TestSampleReader_NilCallback 验证无回调时不 panic
func TestSampleReader_NilCallback(t *testing.T) {
	src := &slowReader{remaining: 128 * 1024, chunk: 32 * 1024, delay: 10 * time.Millisecond}
	sr := newSampleReader(src, "upload", time.Now(), nil)
	if _, err := io.Copy(io.Discard, sr); err != nil {
		t.Fatalf("copy with nil callback error = %v", err)
	}
	if sr.TotalBytes() != 128*1024 {
		t.Errorf("TotalBytes = %d, want %d", sr.TotalBytes(), 128*1024)
	}
}
