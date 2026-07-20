package store

import (
	"strings"
	"testing"
)

const testSitePathValue = "X9k-Qm_2Tz7pLw4Nc8Vb"

func TestSitePath_GetUnset(t *testing.T) {
	s := newTestStore(t)

	p, err := s.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath() error = %v, want nil", err)
	}
	if p != "" {
		t.Errorf("GetSitePath() = %q, want empty when unset", p)
	}
}

func TestSitePath_SetGetRoundTrip(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSitePath(testSitePathValue); err != nil {
		t.Fatalf("SetSitePath() error = %v", err)
	}
	p, err := s.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath() error = %v", err)
	}
	if p != testSitePathValue {
		t.Errorf("GetSitePath() = %q, want %q", p, testSitePathValue)
	}
}

func TestSitePath_SetNormalizes(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSitePath(" /" + testSitePathValue + "/ "); err != nil {
		t.Fatalf("SetSitePath() error = %v", err)
	}
	p, err := s.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath() error = %v", err)
	}
	if p != testSitePathValue {
		t.Errorf("GetSitePath() = %q, want normalized %q", p, testSitePathValue)
	}
}

func TestSitePath_SetInvalid(t *testing.T) {
	s := newTestStore(t)

	for _, bad := range []string{
		"short",                    // 太短
		"abcdefghij1234567890",     // 仅 2 类字符
		"abcdeABCDE12345ab!de",     // 非法字符
		strings.Repeat("aB1-", 17), // 太长(68)
	} {
		if err := s.SetSitePath(bad); err == nil {
			t.Errorf("SetSitePath(%q) = nil, want validation error", bad)
		}
	}

	// 非法写入不得残留任何配置
	p, err := s.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath() error = %v", err)
	}
	if p != "" {
		t.Errorf("GetSitePath() = %q after invalid sets, want empty", p)
	}
}

func TestSitePath_ClearAfterSet(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSitePath(testSitePathValue); err != nil {
		t.Fatalf("SetSitePath() error = %v", err)
	}
	if err := s.SetSitePath(""); err != nil {
		t.Fatalf("SetSitePath(\"\") error = %v", err)
	}
	p, err := s.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath() error = %v", err)
	}
	if p != "" {
		t.Errorf("GetSitePath() = %q after clear, want empty", p)
	}
}
