package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

// stockCodes lists the SKU codes the bearer sees in /api/stock.
func stockCodes(t *testing.T, url, token string) string {
	t.Helper()
	var items []store.StockItem
	call(t, "GET", url+"/api/stock", token, nil, 200, &items)
	var codes []string
	for _, it := range items {
		codes = append(codes, it.Code)
	}
	return strings.Join(codes, ",")
}

func TestAccessTokenTenantUnsetServesDefaultSchema(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	if err := srv.CheckAccessTokenTenant(t.Context()); err != nil {
		t.Fatal(err)
	}
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &tn)
	seedCatalog(t, srv, tn.AccessCode, "ONLY-T")
	if got := stockCodes(t, url, "shared"); strings.Contains(got, "ONLY-T") || got == "" {
		t.Fatalf("shared token should see the default schema, got %q", got)
	}
}

func TestAccessTokenTenantServesTenantSchema(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	body := newBody()
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	srv.AccessTokenTenant = uuid.MustParse(body["tenant_id"])
	if err := srv.CheckAccessTokenTenant(t.Context()); err != nil {
		t.Fatal(err)
	}
	defaultBefore := func() int {
		items, err := srv.Store.Stock(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}()

	seedCatalog(t, srv, tn.AccessCode, "ONLY-T")
	var cat store.Catalog
	call(t, "GET", url+"/api/catalog", "shared", nil, 200, &cat)
	if len(cat.SKUs) != 1 || cat.SKUs[0].Code != "ONLY-T" {
		t.Fatalf("shared token should see the tenant catalog: %+v", cat.SKUs)
	}
	call(t, "POST", url+"/api/movements", "shared", store.NewMovement{
		ID: uuid.New(), Kind: store.KindReceipt, SKUID: cat.SKUs[0].ID, LocationID: cat.Locations[0].ID,
		Qty: 5, Operator: "o", Source: "keyboard",
	}, 201, nil)

	// The write landed in the tenant schema, not in public.
	if got := stockCodes(t, url, tn.AccessCode); got != "ONLY-T" {
		t.Fatalf("tenant code sees %q", got)
	}
	if got := stockCodes(t, url, "shared"); got != "ONLY-T" {
		t.Fatalf("shared token sees %q", got)
	}
	items, err := srv.Store.Stock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != defaultBefore {
		t.Fatalf("default schema changed: %d -> %d items", defaultBefore, len(items))
	}
	for _, it := range items {
		if it.Code == "ONLY-T" {
			t.Fatal("tenant SKU leaked into the default schema")
		}
	}
	call(t, "GET", url+"/api/stock", "wrong", nil, 401, nil)
}

func TestAccessTokenTenantSuspendedIsRefused(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	body := newBody()
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	srv.AccessTokenTenant = uuid.MustParse(body["tenant_id"])

	call(t, "GET", url+"/api/stock", "shared", nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+body["tenant_id"]+"/suspend", provToken, nil, 200, nil)
	call(t, "GET", url+"/api/stock", "shared", nil, 403, nil)
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 403, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+body["tenant_id"]+"/resume", provToken, nil, 200, nil)
	call(t, "GET", url+"/api/stock", "shared", nil, 200, nil)
}

func TestAccessTokenTenantUnknownFailsStartup(t *testing.T) {
	srv, _, _ := adminSetup(t, "shared")
	srv.AccessTokenTenant = uuid.New()
	err := srv.CheckAccessTokenTenant(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no such tenant") || !strings.Contains(err.Error(), srv.AccessTokenTenant.String()) {
		t.Fatalf("want a clear no-such-tenant error, got %v", err)
	}
	if strings.Contains(err.Error(), "shared") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestAccessTokenTenantNeedsAccessToken(t *testing.T) {
	srv, _, _ := adminSetup(t, "")
	srv.AccessTokenTenant = uuid.New()
	if err := srv.CheckAccessTokenTenant(t.Context()); err == nil {
		t.Fatal("tenant mapping without ACCESS_TOKEN must fail startup")
	}
}

func TestAdminRoutesIgnoreAccessTokenTenant(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	body := newBody()
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, nil)
	srv.AccessTokenTenant = uuid.MustParse(body["tenant_id"])
	call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, nil)
	call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], "shared", nil, 401, nil)
}
