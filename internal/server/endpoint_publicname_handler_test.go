package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// putPublicName calls PUT /api/endpoints/{id}/public-name with a raw body.
func putPublicName(t *testing.T, h http.Handler, cookie *http.Cookie, id int64, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT",
		"/api/endpoints/"+strconv.FormatInt(id, 10)+"/public-name", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestCreateEndpointAPI_CarriesPublicName the create route accepts an optional
// public_name and the response (and the store row) carry it; omitting the
// field leaves the name unset.
func TestCreateEndpointAPI_CarriesPublicName(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	body, _ := json.Marshal(map[string]string{"alias": "example.com", "public_name": " 家里宽带 "})
	req := httptest.NewRequest("POST", "/api/endpoints", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var created struct {
		ID         int64  `json:"id"`
		PublicName string `json:"public_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.PublicName != "家里宽带" {
		t.Errorf("create response PublicName = %q, want 家里宽带 (trimmed at store boundary)", created.PublicName)
	}

	// Without the field the name stays unset.
	body2, _ := json.Marshal(map[string]string{"alias": "example.com"})
	req2 := httptest.NewRequest("POST", "/api/endpoints", bytes.NewReader(body2))
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w2.Code, w2.Body.String())
	}
	var created2 struct {
		PublicName string `json:"public_name"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &created2); err != nil {
		t.Fatalf("decode second create response: %v", err)
	}
	if created2.PublicName != "" {
		t.Errorf("PublicName = %q, want empty when the field is omitted", created2.PublicName)
	}
}

// TestUpdateEndpointPublicNameAPI the write lands and the list route echoes it
// back (the list item embeds *store.Endpoint, so the field rides along with no
// extra wiring).
func TestUpdateEndpointPublicNameAPI(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "example.com")

	body, _ := json.Marshal(map[string]string{"public_name": "office line"})
	if w := putPublicName(t, h, cookie, ep.ID, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.PublicName != "office line" {
		t.Errorf("PublicName = %q, want office line", got.PublicName)
	}

	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d (body %s)", w.Code, w.Body.String())
	}
	var list []struct {
		ID         int64  `json:"id"`
		PublicName string `json:"public_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found bool
	for _, item := range list {
		if item.ID != ep.ID {
			continue
		}
		found = true
		if item.PublicName != "office line" {
			t.Errorf("listed PublicName = %q, want office line", item.PublicName)
		}
	}
	if !found {
		t.Fatalf("endpoint %d missing from the list response", ep.ID)
	}
}

// TestUpdateEndpointPublicNameAPI_Clear an empty string clears the name, which
// is how an operator rolls back to the bare brand title.
func TestUpdateEndpointPublicNameAPI_Clear(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "example.com")
	if err := st.UpdateEndpointPublicName(ep.ID, "office line"); err != nil {
		t.Fatalf("seed public name: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"public_name": ""})
	if w := putPublicName(t, h, cookie, ep.ID, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.PublicName != "" {
		t.Errorf("PublicName = %q, want empty after clear", got.PublicName)
	}
}

// TestUpdateEndpointPublicNameAPI_Rejects a malformed body and a non-numeric
// id are 400; the row must stay untouched.
func TestUpdateEndpointPublicNameAPI_Rejects(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "example.com")

	if w := putPublicName(t, h, cookie, ep.ID, []byte("{bad")); w.Code != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"public_name": "x"})
	req := httptest.NewRequest("PUT", "/api/endpoints/abc/public-name", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", w.Code)
	}

	got, _ := st.GetEndpointByID(ep.ID)
	if got.PublicName != "" {
		t.Errorf("rejected requests changed the row: %q", got.PublicName)
	}
}

// TestUpdateEndpointPublicNameAPI_ForeignEndpointIs404 a row owned by another
// user is not addressable, so the route cannot be used to probe or rename it.
func TestUpdateEndpointPublicNameAPI_ForeignEndpointIs404(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	other, err := st.CreateUser("pn-foreign", "$2a$10$hash", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ep, _ := st.CreateEndpointForUser(other.ID, "example.com")

	body, _ := json.Marshal(map[string]string{"public_name": "hostile rename"})
	if w := putPublicName(t, h, cookie, ep.ID, body); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.PublicName != "" {
		t.Errorf("foreign row changed: PublicName = %q, want empty", got.PublicName)
	}
}
