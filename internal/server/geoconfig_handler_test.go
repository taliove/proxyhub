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

// putGeoConfig calls PUT /api/endpoints/{id}/geo-config with a raw body.
func putGeoConfig(t *testing.T, h http.Handler, cookie *http.Cookie, id int64, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT",
		"/api/endpoints/"+strconv.FormatInt(id, 10)+"/geo-config", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestUpdateEndpointGeoConfigAPI the write lands normalised and the list route
// echoes it back, which is what makes the full-replace contract workable for a
// client (it always has the current triple to send).
func TestUpdateEndpointGeoConfigAPI(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "地域配置设备")

	body, _ := json.Marshal(map[string]string{
		"geo_mode": store.GeoModeEnforce, "geo_countries": "cn, jp", "geo_provinces": "Guangdong",
	})
	if w := putGeoConfig(t, h, cookie, ep.ID, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	got, _ := st.GetEndpointByID(ep.ID)
	if got.GeoMode != store.GeoModeEnforce {
		t.Errorf("GeoMode = %q, want enforce", got.GeoMode)
	}
	if got.GeoCountries != "CN,JP" {
		t.Errorf("GeoCountries = %q, want CN,JP", got.GeoCountries)
	}
	if got.GeoProvinces != "Guangdong" {
		t.Errorf("GeoProvinces = %q, want Guangdong", got.GeoProvinces)
	}

	// The list route carries the geo triple so the UI can render current state.
	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d (body %s)", w.Code, w.Body.String())
	}
	var list []struct {
		ID           int64  `json:"id"`
		GeoMode      string `json:"geo_mode"`
		GeoCountries string `json:"geo_countries"`
		GeoProvinces string `json:"geo_provinces"`
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
		if item.GeoMode != store.GeoModeEnforce || item.GeoCountries != "CN,JP" || item.GeoProvinces != "Guangdong" {
			t.Errorf("listed geo config = %q/%q/%q, want enforce/CN,JP/Guangdong",
				item.GeoMode, item.GeoCountries, item.GeoProvinces)
		}
	}
	if !found {
		t.Fatalf("endpoint %d missing from the list response", ep.ID)
	}
}

// TestUpdateEndpointGeoConfigAPI_ClearsBackToOff sending off with empty lists
// returns the address to the inert default, so a rule can be rolled back.
func TestUpdateEndpointGeoConfigAPI_ClearsBackToOff(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "回滚设备")
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeEnforce, "CN", "GD"); err != nil {
		t.Fatalf("seed geo config: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"geo_mode": store.GeoModeOff})
	if w := putGeoConfig(t, h, cookie, ep.ID, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.GeoMode != store.GeoModeOff || got.GeoCountries != "" || got.GeoProvinces != "" {
		t.Errorf("config = %q/%q/%q, want off with both lists cleared",
			got.GeoMode, got.GeoCountries, got.GeoProvinces)
	}
}

// TestUpdateEndpointGeoConfigAPI_Rejects a typo'd mode, a malformed body and a
// non-numeric id are all 400; the row must stay untouched.
func TestUpdateEndpointGeoConfigAPI_Rejects(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "非法输入设备")

	bad, _ := json.Marshal(map[string]string{"geo_mode": "enfroce", "geo_countries": "CN"})
	if w := putGeoConfig(t, h, cookie, ep.ID, bad); w.Code != http.StatusBadRequest {
		t.Errorf("unknown mode status = %d, want 400", w.Code)
	}
	if w := putGeoConfig(t, h, cookie, ep.ID, []byte("{bad")); w.Code != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", w.Code)
	}

	req := httptest.NewRequest("PUT", "/api/endpoints/abc/geo-config", bytes.NewReader(bad))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", w.Code)
	}

	got, _ := st.GetEndpointByID(ep.ID)
	if got.GeoMode != store.GeoModeOff || got.GeoCountries != "" {
		t.Errorf("rejected requests changed the row: %q/%q", got.GeoMode, got.GeoCountries)
	}
}

// TestUpdateEndpointGeoConfigAPI_RequiresAuth an unauthenticated caller cannot
// reach the route at all.
func TestUpdateEndpointGeoConfigAPI_RequiresAuth(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	ep, _ := st.CreateEndpointForUser(1, "未鉴权设备")

	body, _ := json.Marshal(map[string]string{"geo_mode": store.GeoModeEnforce, "geo_countries": "CN"})
	req := httptest.NewRequest("PUT",
		"/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/geo-config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("unauthenticated request succeeded: %d", w.Code)
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.GeoMode != store.GeoModeOff {
		t.Errorf("GeoMode = %q, want off", got.GeoMode)
	}
}

// TestUpdateEndpointGeoConfigAPI_ForeignEndpointIs404 a row owned by another
// user is not addressable, so the route cannot be used to probe or configure it.
func TestUpdateEndpointGeoConfigAPI_ForeignEndpointIs404(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	other, err := st.CreateUser("geo-foreign", "$2a$10$hash", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ep, _ := st.CreateEndpointForUser(other.ID, "他人设备")

	body, _ := json.Marshal(map[string]string{"geo_mode": store.GeoModeEnforce, "geo_countries": "US"})
	if w := putGeoConfig(t, h, cookie, ep.ID, body); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.GeoMode != store.GeoModeOff {
		t.Errorf("foreign row changed: GeoMode = %q, want off", got.GeoMode)
	}
}
