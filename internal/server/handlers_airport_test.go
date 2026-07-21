package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// TestCreateAirport_EmptyAbbr tests auto-generation when abbr is empty
func TestCreateAirport_EmptyAbbr(t *testing.T) {
	s, _ := newTestServer(t, nil)

	tests := []struct {
		name         string
		airportName  string
		abbr         string // input abbr (empty = auto-generate)
		wantAbbrLike string // expected abbr pattern (e.g., "JS" for "极速机场")
	}{
		{
			name:         "chinese name with generic suffix",
			airportName:  "极速机场",
			abbr:         "",
			wantAbbrLike: "JS",
		},
		{
			name:         "latin name",
			airportName:  "FlowerCloud",
			abbr:         "",
			wantAbbrLike: "FC",
		},
		{
			name:         "explicit abbr not overwritten",
			airportName:  "极速机场",
			abbr:         "JISU",
			wantAbbrLike: "JISU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody, _ := json.Marshal(map[string]string{
				"name": tt.airportName,
				"url":  "https://example.com/sub",
				"abbr": tt.abbr,
			})
			req := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(reqBody))
			w := httptest.NewRecorder()

			s.handleCreateAirport(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want status 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp store.Airport
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if resp.Abbr != tt.wantAbbrLike {
				t.Errorf("abbr = %q, want %q", resp.Abbr, tt.wantAbbrLike)
			}

			// Verify abbr is persisted in database
			airport, err := s.st.GetAirportByID(resp.ID)
			if err != nil {
				t.Fatalf("get airport from db: %v", err)
			}
			if airport.Abbr != tt.wantAbbrLike {
				t.Errorf("db abbr = %q, want %q", airport.Abbr, tt.wantAbbrLike)
			}
		})
	}
}

// TestCreateAirport_AbbrDeduplication tests that duplicate abbr gets suffix
func TestCreateAirport_AbbrDeduplication(t *testing.T) {
	s, _ := newTestServer(t, nil)

	// Create first airport with abbr "JS"
	reqBody1, _ := json.Marshal(map[string]string{
		"name": "极速机场",
		"url":  "https://example.com/sub1",
		"abbr": "JS",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(reqBody1))
	w1 := httptest.NewRecorder()
	s.handleCreateAirport(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first create failed: %d", w1.Code)
	}

	// Create second airport that would generate "JS" -> should get "JS2"
	reqBody2, _ := json.Marshal(map[string]string{
		"name": "疾速机场", // also generates "JS"
		"url":  "https://example.com/sub2",
		"abbr": "",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(reqBody2))
	w2 := httptest.NewRecorder()
	s.handleCreateAirport(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second create failed: %d", w2.Code)
	}

	var resp store.Airport
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Abbr != "JS2" {
		t.Errorf("abbr = %q, want JS2 (deduplicated)", resp.Abbr)
	}
}

// TestUpdateAirport_ClearAbbr tests auto-generation when clearing abbr
func TestUpdateAirport_ClearAbbr(t *testing.T) {
	s, _ := newTestServer(t, nil)

	// Create airport with explicit abbr
	airport, err := s.st.CreateAirport("极速机场", "https://example.com/sub")
	if err != nil {
		t.Fatalf("create airport: %v", err)
	}
	if err := s.st.UpdateAirport(airport.ID, airport.Name, airport.URL, "CUSTOM"); err != nil {
		t.Fatalf("set custom abbr: %v", err)
	}

	// Update with empty abbr -> should auto-generate
	reqBody, _ := json.Marshal(map[string]string{
		"name": "极速机场",
		"url":  "https://example.com/sub",
		"abbr": "",
	})
	req := httptest.NewRequest(http.MethodPut, "/airports/1", bytes.NewReader(reqBody))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.handleUpdateAirport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify abbr is auto-generated in database
	updated, err := s.st.GetAirportByID(airport.ID)
	if err != nil {
		t.Fatalf("get updated airport: %v", err)
	}
	if updated.Abbr != "JS" {
		t.Errorf("abbr = %q, want JS (auto-generated)", updated.Abbr)
	}
}

