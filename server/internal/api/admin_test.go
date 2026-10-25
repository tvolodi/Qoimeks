package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

const provToken = "prov-token-for-tests"

type tenantBody struct {
	TenantID   string `json:"tenant_id"`
	Status     string `json:"status"`
	ServerURL  string `json:"server_url"`
	AccessCode string `json:"access_code"`
}

// adminSetup starts a server with the provisioning API on and a controllable clock.
func adminSetup(t *testing.T, accessToken string) (*Server, string, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		Store:             storetest.New(t),
		AccessToken:       accessToken,
		ProvisioningToken: provToken,
		PublicURL:         "https://qoimeks.example/",
		now:               func() time.Time { return clock },
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return srv, ts.URL, &clock
}

func call(t *testing.T, method, url, token string, body any, want int, out any) {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		json.NewEncoder(&buf).Encode(b)
	}
	req, _ := http.NewRequest(method, url, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	json.NewDecoder(resp.Body).Decode(&raw)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, url, resp.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, url, err, raw)
		}
	}
}

// seedCatalog gives a tenant one SKU at one location, through the store's import.
func seedCatalog(t *testing.T, srv *Server, code, sku string) {
	t.Helper()
	tn, err := srv.Store.TenantByCode(t.Context(), code)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := srv.tenantStore(t.Context(), tn.SchemaName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.Import(t.Context(), []store.ImportLocation{{Code: "L"}},
		[]store.ImportSKU{{Code: sku, Name: sku, Home: "L"}}, store.ImportOptions{Operator: "o"})
	if err != nil {
		t.Fatal(err)
	}
}

func newBody() map[string]string {
	return map[string]string{
		"tenant_id": uuid.NewString(), "name": "Склад", "language": "ru", "tier": "demo", "owner_email": "o@example.com",
	}
}

func TestCreateTenantTwice(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	body := newBody()
	var first, second tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &first)
	var raw map[string]any
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 200, &second)
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 200, &raw)
	if _, has := raw["access_code"]; has || second.AccessCode != "" {
		t.Fatalf("repeat returned a code: %+v", raw)
	}
	withoutCode := first
	withoutCode.AccessCode = ""
	if withoutCode != second {
		t.Fatalf("bodies differ: %+v vs %+v", first, second)
	}
	if first.Status != "active" || first.AccessCode == "" || first.TenantID != body["tenant_id"] || first.ServerURL != "https://qoimeks.example" {
		t.Fatalf("bad body: %+v", first)
	}
	var got struct {
		TenantID  string `json:"tenant_id"`
		Status    string `json:"status"`
		Tier      string `json:"tier"`
		CreatedAt string `json:"created_at"`
	}
	call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, &got)
	if got.Status != "active" || got.Tier != "demo" || got.CreatedAt == "" {
		t.Fatalf("GET: %+v", got)
	}
}

func TestCreateTenantTierAndPlan(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	for _, c := range []struct{ tier, plan, wantTier, wantPlan, stored string }{
		{"demo", "", "demo", "", "demo"},
		{"demo", "all_inclusive", "demo", "", "demo"},
		{"production", "", "production", "own_keys", "hosted_own_keys"},
		{"production", "own_keys", "production", "own_keys", "hosted_own_keys"},
		{"production", "all_inclusive", "production", "all_inclusive", "hosted_all_inclusive"},
		{"hosted_own_keys", "all_inclusive", "production", "own_keys", "hosted_own_keys"},
		{"hosted_all_inclusive", "", "production", "all_inclusive", "hosted_all_inclusive"},
	} {
		t.Run(c.tier+"/"+c.plan, func(t *testing.T) {
			body := map[string]string{
				"tenant_id": uuid.NewString(), "name": "Склад", "language": "ru", "tier": c.tier,
				"hostname": "acme.qoimeks.ai-dala.com",
			}
			if c.plan != "" {
				body["plan"] = c.plan
			}
			call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, nil)
			var got map[string]any
			call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, &got)
			var wantPlan any
			if c.wantPlan != "" {
				wantPlan = c.wantPlan
			}
			if got["tier"] != c.wantTier || got["plan"] != wantPlan || got["stored_tier"] != c.stored {
				t.Fatalf("GET: %+v", got)
			}
			if _, has := got["hostname"]; has {
				t.Fatalf("hostname was stored: %+v", got)
			}
			// Repeat with other tier and plan: idempotent, nothing changes.
			body["tier"], body["plan"] = "production", "all_inclusive"
			call(t, "POST", url+"/admin/v1/tenants", provToken, body, 200, nil)
			var again map[string]any
			call(t, "GET", url+"/admin/v1/tenants/"+body["tenant_id"], provToken, nil, 200, &again)
			if again["stored_tier"] != c.stored {
				t.Fatalf("repeat changed the tier: %+v", again)
			}
		})
	}
}

func TestRotateCode(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	body := newBody()
	id := body["tenant_id"]
	var tn, rot tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/rotate-code", "", nil, 401, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/rotate-code", "wrong", nil, 401, nil)
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, nil)

	call(t, "POST", url+"/admin/v1/tenants/"+id+"/suspend", provToken, nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/rotate-code", provToken, nil, 200, &rot)
	if rot.AccessCode == "" || rot.AccessCode == tn.AccessCode {
		t.Fatalf("rotated code: %+v", rot)
	}
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/resume", provToken, nil, 200, nil)
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 401, nil)
	call(t, "GET", url+"/api/stock", rot.AccessCode, nil, 200, nil)

	var raw map[string]any
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 200, &raw)
	if _, has := raw["access_code"]; has {
		t.Fatal("repeat POST leaked a code after rotation")
	}
	call(t, "POST", url+"/admin/v1/tenants/"+uuid.NewString()+"/rotate-code", provToken, nil, 404, nil)
	call(t, "POST", url+"/admin/v1/tenants/not-a-uuid/rotate-code", provToken, nil, 404, nil)
}

func TestSuspendedTenantIsRefusedAndResumeRestores(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	body := newBody()
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	id := body["tenant_id"]

	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/suspend", provToken, nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/suspend", provToken, nil, 200, nil) // idempotent
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 403, nil)
	call(t, "GET", url+"/api/catalog", tn.AccessCode, nil, 403, nil)
	call(t, "GET", url+"/api/stock", "shared", nil, 200, nil) // the default schema is unaffected
	var st struct{ Status string }
	call(t, "GET", url+"/admin/v1/tenants/"+id, provToken, nil, 200, &st)
	if st.Status != "suspended" {
		t.Fatalf("status %q", st.Status)
	}
	call(t, "POST", url+"/admin/v1/tenants/"+id+"/resume", provToken, nil, 200, &st)
	if st.Status != "active" {
		t.Fatalf("status %q", st.Status)
	}
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, nil)
}

func TestSuspendKeepsData(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	body := newBody()
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, body, 201, &tn)
	seedCatalog(t, srv, tn.AccessCode, "KEEP")
	call(t, "POST", url+"/admin/v1/tenants/"+body["tenant_id"]+"/suspend", provToken, nil, 200, nil)
	call(t, "POST", url+"/admin/v1/tenants/"+body["tenant_id"]+"/resume", provToken, nil, 200, nil)
	var items []store.StockItem
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, &items)
	if len(items) != 1 || items[0].Code != "KEEP" {
		t.Fatalf("stock after resume: %+v", items)
	}
}

func TestAdminRequiresProvisioningToken(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	id := uuid.NewString()
	routes := []struct{ method, path string }{
		{"POST", "/admin/v1/tenants"},
		{"POST", "/admin/v1/tenants/" + id + "/suspend"},
		{"POST", "/admin/v1/tenants/" + id + "/resume"},
		{"GET", "/admin/v1/tenants/" + id},
	}
	for _, r := range routes {
		for _, token := range []string{"", "wrong", "shared"} { // the app's ACCESS_TOKEN is not enough
			var e map[string]string
			call(t, r.method, url+r.path, token, newBody(), 401, &e)
			if e["error"] != "unauthorized" {
				t.Fatalf("%s %s: %v", r.method, r.path, e)
			}
		}
	}
}

func TestAdminRateLimit(t *testing.T) {
	_, url, clock := adminSetup(t, "shared")
	id := url + "/admin/v1/tenants/" + uuid.NewString()
	for range 30 {
		call(t, "GET", id, provToken, nil, 404, nil)
	}
	resp, _ := http.NewRequest("GET", id, nil)
	resp.Header.Set("Authorization", "Bearer "+provToken)
	r, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("31st call: %d retry-after=%q", r.StatusCode, r.Header.Get("Retry-After"))
	}
	// Unauthorized calls are not counted and do not consume the window.
	call(t, "GET", id, "wrong", nil, 401, nil)
	*clock = clock.Add(61 * time.Second)
	call(t, "GET", id, provToken, nil, 404, nil)
}

func TestAdminOffWithoutToken(t *testing.T) {
	srv := &Server{Store: storetest.New(t)}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	call(t, "POST", ts.URL+"/admin/v1/tenants", "", newBody(), 404, nil)
	call(t, "POST", ts.URL+"/admin/v1/tenants", "anything", newBody(), 404, nil)
}

func TestAdminBadInput(t *testing.T) {
	_, url, _ := adminSetup(t, "shared")
	mod := func(k, v string) map[string]string { b := newBody(); b[k] = v; return b }
	for name, body := range map[string]any{
		"bad uuid":     mod("tenant_id", "x"),
		"bad language": mod("language", "de"),
		"bad tier":     mod("tier", "gold"),
		"bad plan":     func() map[string]string { b := mod("tier", "production"); b["plan"] = "gold"; return b }(),
		"not json":     "{nope",
		"oversized":    mod("name", strings.Repeat("a", 9<<10)),
	} {
		t.Run(name, func(t *testing.T) {
			var e map[string]string
			call(t, "POST", url+"/admin/v1/tenants", provToken, body, 400, &e)
			if e["error"] != "invalid_request" {
				t.Fatalf("error = %v", e)
			}
		})
	}
	for _, path := range []string{"/suspend", "/resume", ""} {
		method := "POST"
		if path == "" {
			method = "GET"
		}
		call(t, method, url+"/admin/v1/tenants/"+uuid.NewString()+path, provToken, nil, 404, nil)
		call(t, method, url+"/admin/v1/tenants/not-a-uuid"+path, provToken, nil, 404, nil)
	}
}

func TestTenantsDoNotSeeEachOther(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	var a, b tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &a)
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &b)

	post := func(code, sku string) {
		t.Helper()
		var cat store.Catalog
		call(t, "GET", url+"/api/catalog", code, nil, 200, &cat)
		if len(cat.SKUs) != 0 {
			t.Fatalf("new tenant catalog is not empty: %+v", cat.SKUs)
		}
		seedCatalog(t, srv, code, sku)
		call(t, "GET", url+"/api/catalog", code, nil, 200, &cat)
		call(t, "POST", url+"/api/movements", code, store.NewMovement{
			ID: uuid.New(), Kind: store.KindReceipt, SKUID: cat.SKUs[0].ID, LocationID: cat.Locations[0].ID,
			Qty: 7, Operator: "o", Source: "keyboard",
		}, 201, nil)
	}
	post(a.AccessCode, "ONLY-A")
	post(b.AccessCode, "ONLY-B")

	seen := func(code string) string {
		var items []store.StockItem
		call(t, "GET", url+"/api/stock", code, nil, 200, &items)
		var codes []string
		for _, it := range items {
			codes = append(codes, it.Code)
		}
		return strings.Join(codes, ",")
	}
	if got := seen(a.AccessCode); got != "ONLY-A" {
		t.Fatalf("tenant A sees %q", got)
	}
	if got := seen(b.AccessCode); got != "ONLY-B" {
		t.Fatalf("tenant B sees %q", got)
	}
	if got := seen("shared"); strings.Contains(got, "ONLY") {
		t.Fatalf("default schema sees tenant stock: %q", got)
	}
	call(t, "GET", url+"/api/stock", "unknown-code", nil, 401, nil)
	call(t, "GET", url+"/api/stock", "", nil, 401, nil)
}

func TestEmptyAccessTokenKeepsDefaultOpen(t *testing.T) {
	_, url, _ := adminSetup(t, "")
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &tn)

	var def, mine []store.StockItem
	call(t, "GET", url+"/api/stock", "", nil, 200, &def) // no header: default schema, as today
	call(t, "GET", url+"/api/stock", "garbage", nil, 200, &def)
	call(t, "GET", url+"/api/stock", tn.AccessCode, nil, 200, &mine)
	if len(def) == 0 {
		t.Fatal("default schema should hold the demo catalog")
	}
	if len(mine) != 0 {
		t.Fatalf("tenant code was served from the default schema: %+v", mine)
	}
}

func TestRateLimiterWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &rateLimiter{n: 3, window: time.Minute, now: func() time.Time { return now }}
	steps := []struct {
		advance time.Duration
		want    bool
	}{
		{0, true}, {10 * time.Second, true}, {10 * time.Second, true},
		{10 * time.Second, false}, // 4th within the minute
		{29 * time.Second, false}, // t=59s, first event still inside
		{time.Second, true},       // t=60s, first event left
		{0, false},                // window full again
		{60 * time.Second, true},  // everything left
	}
	for i, s := range steps {
		now = now.Add(s.advance)
		if got, _ := l.allow(); got != s.want {
			t.Fatalf("step %d: allow = %v, want %v", i, got, s.want)
		}
	}
}
