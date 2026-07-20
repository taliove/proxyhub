package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResolveOnce(t *testing.T) {
	st := newTestStore(t)

	// 造两条未解析的拉取记录
	ep, _ := st.CreateEndpoint("测试")
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "1.2.3.4"})
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "5.6.7.8"})

	// Mock geo API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","country":"中国","regionName":"广东","city":"深圳","isp":"电信"}`)
	}))
	defer ts.Close()

	resolver := NewResolver(st, ts.URL)
	resolved, err := resolver.ResolveOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ResolveOnce() error = %v", err)
	}
	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}

	geo, err := st.GetGeo("1.2.3.4")
	if err != nil {
		t.Fatalf("GetGeo() error = %v", err)
	}
	if geo.City != "深圳" {
		t.Errorf("City = %s, want 深圳", geo.City)
	}

	// 再跑一次应该没有未解析的了
	resolved, _ = resolver.ResolveOnce(context.Background(), 10)
	if resolved != 0 {
		t.Errorf("second run resolved = %d, want 0", resolved)
	}
}

func TestResolveOnce_APIFailure(t *testing.T) {
	st := newTestStore(t)

	ep, _ := st.CreateEndpoint("测试")
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "1.2.3.4"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	resolver := NewResolver(st, ts.URL)
	resolved, err := resolver.ResolveOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ResolveOnce() error = %v", err)
	}
	if resolved != 0 {
		t.Errorf("resolved = %d, want 0", resolved)
	}
}

func TestResolver_LookupIP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","country":"日本","regionName":"Tokyo","city":"Tokyo","isp":"x"}`))
	}))
	defer ts.Close()

	r := NewResolver(nil, ts.URL)
	geo, err := r.LookupIP(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("LookupIP: %v", err)
	}
	if geo.Country != "日本" {
		t.Errorf("Country = %q, want 日本", geo.Country)
	}
}
