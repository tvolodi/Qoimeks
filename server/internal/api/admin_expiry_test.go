package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateTenantExpiresAt(t *testing.T) {
	_, url, clock := adminSetup(t, "shared")
	future := clock.Add(time.Hour).Format(time.RFC3339)
	past := clock.Add(-time.Hour).Format(time.RFC3339)

	body := newBody()
	body["expires_at"] = future
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, nil)
	var got map[string]any
	call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, &got)
	if got["expires_at"] != future || got["status"] != "active" {
		t.Fatalf("GET: %v", got)
	}
	// Idempotent repeat ignores a changed date.
	body["expires_at"] = clock.Add(5 * time.Hour).Format(time.RFC3339)
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 200, nil)
	call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, &got)
	if got["expires_at"] != future {
		t.Fatalf("repeat changed expires_at: %v", got)
	}

	plain := newBody()
	call(t, "POST", url+"/admin/v1/tenants", provToken, plain, 201, nil)
	got = nil
	call(t, "GET", url+"/admin/v1/tenants/"+plain["tenant_id"], provToken, nil, 200, &got)
	if v, has := got["expires_at"]; !has || v != nil {
		t.Fatalf("expires_at should be null: %v", got)
	}

	tests := map[string]func(map[string]string){
		"past":       func(b map[string]string) { b["expires_at"] = past },
		"production": func(b map[string]string) { b["tier"] = "production"; b["expires_at"] = future },
		"legacy":     func(b map[string]string) { b["tier"] = "hosted_own_keys"; b["expires_at"] = future },
		"not a date": func(b map[string]string) { b["expires_at"] = "tomorrow" },
	}
	for name, mod := range tests {
		b := newBody()
		mod(b)
		var e map[string]string
		call(t, "POST", url+"/admin/v1/tenants", provToken, b, 400, &e)
		if e["error"] != "invalid_request" {
			t.Fatalf("%s: %v", name, e)
		}
	}
}

func TestExpireTenant(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	body := newBody()
	id := body["tenant_id"]
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)

	call(t, "POST", url+"/admin/v1/tenants/"+id+"/expire", "wrong", nil, 401, nil)
	var st struct{ Status string }
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/expire", provToken, nil, 200, &st)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/expire", provToken, nil, 200, &st) // idempotent
	if st.Status != "expired" {
		t.Fatalf("status %q", st.Status)
	}
	var e map[string]string
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 403, &e)
	if e["error"] != "tenant expired" {
		t.Fatalf("api error: %v", e)
	}
	call(t, "GET", url+"/api/stock", "shared", nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/resume", provToken, nil, 409, &e)
	if e["error"] != "conflict" || e["message"] != "tenant expired" {
		t.Fatalf("resume: %v", e)
	}
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/suspend", provToken, nil, 409, nil)
	st.Status = ""
	call(t, "GET", url+"/admin/v1/tenants/"+id, provToken, nil, 200, &st)
	if st.Status != "expired" {
		t.Fatalf("GET status %q", st.Status)
	}
	call(t, "POST", url+"/admin/v1/tenants/"+uuid.NewString()+"/expire", provToken, nil, 404, nil)
	call(t, "POST", url+"/admin/v1/tenants/not-a-uuid/expire", provToken, nil, 404, nil)
}

func TestDemoTenantPastDateIsRefusedAndKeepsData(t *testing.T) {
	srv, url, clock := adminSetup(t, "shared")
	body := newBody()
	id := body["tenant_id"]
	body["expires_at"] = clock.Add(time.Hour).Format(time.RFC3339)
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	seedCatalog(t, srv, tn.AccessCode, "KEEP")
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, nil)

	*clock = clock.Add(2 * time.Hour)
	var e map[string]string
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 403, &e)
	if e["error"] != "tenant expired" {
		t.Fatalf("api error: %v", e)
	}
	var st struct{ Status string }
	call(t, "GET", url+"/admin/v1/tenants/"+id, provToken, nil, 200, &st)
	if st.Status != "expired" {
		t.Fatalf("derived status %q", st.Status)
	}
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/resume", provToken, nil, 409, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/suspend", provToken, nil, 409, nil)

	// The row is not rewritten, and the data is back once the clock is before the date.
	tenantID, _ := uuid.Parse(id)
	if row, err := srv.Store.Tenant(t.Context(), tenantID); err != nil || row.Status != "active" {
		t.Fatalf("stored status %q %v", row.Status, err)
	}
	*clock = clock.Add(-2 * time.Hour)
	var items []map[string]any
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, &items)
	if len(items) != 1 {
		t.Fatalf("stock after expiry check: %v", items)
	}
}
