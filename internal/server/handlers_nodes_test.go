package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestHandleNodeShareURI(t *testing.T) {
	// Add a test node to the pool
	testNode := &subscription.Node{
		Name:   "Test Node",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		UUID:   "00000000-0000-0000-0000-000000000000",
		Region: "Test",
		Source: "test-source",
	}

	s, _ := newTestServer(t, []*subscription.Node{testNode})

	tests := []struct {
		name       string
		nodeKey    string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "valid node key",
			nodeKey:    testNode.NodeKey(),
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "non-existent node key",
			nodeKey:    "nonexistent.example.com:999",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "empty node key",
			nodeKey:    "",
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/nodes/" + tt.nodeKey + "/share-uri"
			req := httptest.NewRequest("GET", path, nil)
			req.SetPathValue("nodeKey", tt.nodeKey)
			rec := httptest.NewRecorder()

			s.handleNodeShareURI(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if !tt.wantErr {
				var res map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if res["uri"] == "" {
					t.Error("expected non-empty uri in response")
				}
				// Verify it's a valid share link format
				if len(res["uri"]) < 10 || res["uri"][:6] != "vless:" {
					t.Errorf("invalid share URI format: %s", res["uri"])
				}
			}
		})
	}
}

