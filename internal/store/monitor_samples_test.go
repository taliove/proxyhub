package store

import (
	"testing"
	"time"
)

func TestMonitorSamplesRoundtrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	if err := s.SaveMonitorSample("a.example.com:443", true, 88, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMonitorSample("a.example.com:443", false, 0, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	// 窗口外旧数据:prune 后应消失
	if err := s.SaveMonitorSample("a.example.com:443", true, 50, now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	samples, err := s.ListMonitorSamples("a.example.com:443", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 3 {
		t.Fatalf("samples = %d, want 3", len(samples))
	}
	// 新到旧
	if !samples[0].OK || samples[0].LatencyMs != 88 {
		t.Errorf("latest = %+v, want ok/88ms", samples[0])
	}
	if samples[1].OK {
		t.Errorf("second = %+v, want failed sample", samples[1])
	}

	if err := s.PruneMonitorSamples(now.Add(-7 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	samples, _ = s.ListMonitorSamples("a.example.com:443", 10)
	if len(samples) != 2 {
		t.Errorf("after prune samples = %d, want 2", len(samples))
	}
}
