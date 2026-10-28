package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func maintenanceServer(t *testing.T, on bool) (*Server, *httptest.Server) {
	t.Helper()
	srv := &Server{
		Store:             storetest.New(t),
		AccessToken:       "code",
		ProvisioningToken: provToken,
		Maintenance:       on,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return srv, ts
}

func sendReq(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestMaintenanceRefusesWrites(t *testing.T) {
	srv, ts := maintenanceServer(t, true)
	tenant := uuid.New().String()
	writes := []struct{ name, method, path, token string }{
		{"post movement", "POST", "/api/movements", "code"},
		{"reverse", "POST", "/api/movements/" + uuid.New().String() + "/reverse", "code"},
		{"import load", "POST", "/api/import/apply", "code"},
		{"import cancel", "POST", "/api/imports/1/cancel", "code"},
		{"feedback", "POST", "/api/feedback", "code"},
		{"parse", "POST", "/api/parse", "code"},
		{"settings", "PUT", "/api/settings/providers", "code"},
		{"tenant create", "POST", "/admin/v1/tenants", provToken},
		{"tenant suspend", "POST", "/admin/v1/tenants/" + tenant + "/suspend", provToken},
		{"tenant resume", "POST", "/admin/v1/tenants/" + tenant + "/resume", provToken},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			resp := sendReq(t, w.method, ts.URL+w.path, w.token, map[string]string{})
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want 503", resp.StatusCode)
			}
			if resp.Header.Get("Retry-After") == "" {
				t.Fatal("no Retry-After header")
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["error"] == nil {
				t.Fatalf("no error message: %v", body)
			}
			if _, ok := body["source"]; ok {
				t.Fatalf("body carries a source: %v", body)
			}
		})
	}

	// The refused post wrote nothing.
	cat, err := srv.Store.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resp := sendReq(t, "POST", ts.URL+"/api/movements", "code", store.NewMovement{
		ID: uuid.New(), Kind: "receipt", SKUID: cat.SKUs[0].ID, LocationID: cat.Locations[0].ID, Qty: 5, Operator: "x",
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.StatusCode)
	}
	ms, err := srv.Store.Movements(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("%d movements written during maintenance", len(ms))
	}
}

func TestMaintenanceKeepsReads(t *testing.T) {
	_, ts := maintenanceServer(t, true)
	for _, path := range []string{"/healthz", "/api/stock", "/api/catalog"} {
		if resp := sendReq(t, "GET", ts.URL+path, "code", nil); resp.StatusCode != 200 {
			t.Fatalf("GET %s: status %d, want 200", path, resp.StatusCode)
		}
	}
	resp := sendReq(t, "OPTIONS", ts.URL+"/api/movements", "", nil)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: status %d, headers %v", resp.StatusCode, resp.Header)
	}
}

func TestMaintenanceOffChangesNothing(t *testing.T) {
	srv, ts := maintenanceServer(t, false)
	cat, err := srv.Store.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resp := sendReq(t, "POST", ts.URL+"/api/movements", "code", store.NewMovement{
		ID: uuid.New(), Kind: "receipt", SKUID: cat.SKUs[0].ID, LocationID: cat.Locations[0].ID, Qty: 5, Operator: "x",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "" {
		t.Fatal("unexpected Retry-After")
	}
}
