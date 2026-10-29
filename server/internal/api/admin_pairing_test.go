package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

type pairingBody struct {
	PairingID string `json:"pairing_id"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
	URL       string `json:"url"`
	PairURI   string `json:"pair_uri"`
}

type deviceBody struct {
	DeviceID   string  `json:"device_id"`
	Label      string  `json:"label"`
	Operator   string  `json:"operator"`
	AppVersion string  `json:"app_version"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func newAdminTenant(t *testing.T, base string) tenantBody {
	t.Helper()
	var tb tenantBody
	call(t, "POST", base+"/admin/v1/tenants", provToken, map[string]string{"tenant_id": uuid.NewString(), "name": "T", "language": "ru", "tier": "production", "plan": "own_keys"}, 201, &tb)
	return tb
}

func pairingURL(base, tenant string, parts ...string) string {
	return base + "/admin/v1/tenants/" + tenant + "/" + strings.Join(parts, "/")
}

// pairDevice makes a device for the tenant through the store, as the exchange will.
func pairDevice(t *testing.T, srv *Server, tenant string, now time.Time) store.Device {
	t.Helper()
	id := uuid.MustParse(tenant)
	_, code, err := srv.Store.CreatePairing(t.Context(), id, "", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := srv.Store.ExchangePairing(t.Context(), code, store.DeviceInfo{Label: "Samsung A15", Operator: "Aidar", AppVersion: "0.2.0"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCreatePairing(t *testing.T) {
	_, base, clock := adminSetup(t, "")
	tn := newAdminTenant(t, base)

	var p pairingBody
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), provToken, map[string]any{"label": "Warehouse phone 2", "ttl_seconds": 120}, 201, &p)
	if _, err := uuid.Parse(p.PairingID); err != nil {
		t.Fatalf("pairing_id %q", p.PairingID)
	}
	if _, ok := store.NormalizePairingCode(p.Code); !ok || len(p.Code) != 9 || p.Code[4] != '-' {
		t.Fatalf("code %q", p.Code)
	}
	if want := clock.Add(2 * time.Minute).UTC().Format(time.RFC3339); p.ExpiresAt != want {
		t.Fatalf("expires_at %s, want %s", p.ExpiresAt, want)
	}
	if p.URL != "https://qoimeks.example" {
		t.Fatalf("url %q", p.URL)
	}
	want := "qoimeks://pair?url=" + url.QueryEscape("https://qoimeks.example") + "&pair=" + p.Code
	if p.PairURI != want {
		t.Fatalf("pair_uri %q, want %q", p.PairURI, want)
	}

	// Empty body: defaults (10 minutes); each call is a new code.
	var p2 pairingBody
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), provToken, nil, 201, &p2)
	if p2.Code == p.Code || p2.PairingID == p.PairingID {
		t.Fatal("second call must make a new code")
	}
	if want := clock.Add(10 * time.Minute).UTC().Format(time.RFC3339); p2.ExpiresAt != want {
		t.Fatalf("default expires_at %s, want %s", p2.ExpiresAt, want)
	}
}

func TestCreatePairingErrors(t *testing.T) {
	srv, base, clock := adminSetup(t, "")
	tn := newAdminTenant(t, base)
	u := pairingURL(base, tn.TenantID, "pairings")

	call(t, "POST", u, "", nil, 401, nil)
	call(t, "POST", u, "wrong", nil, 401, nil)
	call(t, "POST", u, provToken, "{not json", 400, nil)
	call(t, "POST", u, provToken, map[string]any{"label": strings.Repeat("x", 81)}, 400, nil)
	call(t, "POST", u, provToken, map[string]any{"ttl_seconds": 59}, 400, nil)
	call(t, "POST", u, provToken, map[string]any{"ttl_seconds": 901}, 400, nil)
	var e map[string]string
	call(t, "POST", pairingURL(base, uuid.NewString(), "pairings"), provToken, nil, 404, &e)
	if e["error"] != "not_found" {
		t.Fatalf("error %v", e)
	}
	call(t, "POST", pairingURL(base, "not-a-uuid", "pairings"), provToken, nil, 404, nil)

	// Five live codes, the sixth is refused; a cancelled one frees a slot.
	var first pairingBody
	for i := 0; i < store.MaxPendingPairings; i++ {
		var p pairingBody
		call(t, "POST", u, provToken, nil, 201, &p)
		if i == 0 {
			first = p
		}
	}
	call(t, "POST", u, provToken, nil, 409, &e)
	if e["error"] != "too_many_pending" {
		t.Fatalf("error %v", e)
	}
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings", first.PairingID, "cancel"), provToken, nil, 200, nil)
	call(t, "POST", u, provToken, nil, 201, nil)

	// 50 active devices: no new code.
	tn2 := newAdminTenant(t, base)
	var firstDev store.Device
	for i := 0; i < store.MaxDevices; i++ {
		d := pairDevice(t, srv, tn2.TenantID, *clock)
		if i == 0 {
			firstDev = d
		}
	}
	u2 := pairingURL(base, tn2.TenantID, "pairings")
	call(t, "POST", u2, provToken, nil, 409, &e)
	if e["error"] != "too_many_pending" {
		t.Fatalf("error %v", e)
	}
	call(t, "POST", pairingURL(base, tn2.TenantID, "devices", firstDev.ID.String(), "revoke"), provToken, nil, 200, nil)
	call(t, "POST", u2, provToken, nil, 201, nil)
}

func TestCreatePairingRefusedForSuspendedAndExpiredTenant(t *testing.T) {
	_, base, clock := adminSetup(t, "")
	tn := newAdminTenant(t, base)
	u := pairingURL(base, tn.TenantID, "pairings")

	call(t, "POST", pairingURL(base, tn.TenantID, "suspend"), provToken, nil, 200, nil)
	var e map[string]string
	call(t, "POST", u, provToken, nil, 409, &e)
	if e["error"] != "conflict" || !strings.Contains(e["message"], "suspended") {
		t.Fatalf("suspended: %v", e)
	}
	call(t, "POST", pairingURL(base, tn.TenantID, "resume"), provToken, nil, 200, nil)
	call(t, "POST", u, provToken, nil, 201, nil)

	call(t, "POST", pairingURL(base, tn.TenantID, "expire"), provToken, nil, 200, nil)
	call(t, "POST", u, provToken, nil, 409, &e)
	if e["error"] != "conflict" || !strings.Contains(e["message"], "expired") {
		t.Fatalf("expired: %v", e)
	}

	// A demo tenant whose expiry time has passed is refused too.
	var demo tenantBody
	exp := clock.Add(time.Hour).UTC().Format(time.RFC3339)
	call(t, "POST", base+"/admin/v1/tenants", provToken, map[string]string{"tenant_id": uuid.NewString(), "name": "demo", "language": "ru", "tier": "demo", "expires_at": exp}, 201, &demo)
	call(t, "POST", pairingURL(base, demo.TenantID, "pairings"), provToken, nil, 201, nil)
	*clock = clock.Add(2 * time.Hour)
	call(t, "POST", pairingURL(base, demo.TenantID, "pairings"), provToken, nil, 409, &e)
	if e["error"] != "conflict" {
		t.Fatalf("lapsed demo: %v", e)
	}
}

func TestCancelPairing(t *testing.T) {
	srv, base, clock := adminSetup(t, "")
	tn := newAdminTenant(t, base)
	var p pairingBody
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), provToken, nil, 201, &p)
	cancel := pairingURL(base, tn.TenantID, "pairings", p.PairingID, "cancel")

	call(t, "POST", cancel, "", nil, 401, nil)
	var out map[string]string
	call(t, "POST", cancel, provToken, nil, 200, &out)
	first := clock.UTC().Format(time.RFC3339)
	if out["pairing_id"] != p.PairingID || out["cancelled_at"] != first {
		t.Fatalf("cancel: %v", out)
	}
	// The cancelled code no longer works.
	if _, _, err := srv.Store.ExchangePairing(t.Context(), p.Code, store.DeviceInfo{Label: "x"}, *clock); err == nil {
		t.Fatal("cancelled code was accepted")
	}
	// A repeat is the same 200, with the first time.
	*clock = clock.Add(time.Minute)
	call(t, "POST", cancel, provToken, nil, 200, &out)
	if out["cancelled_at"] != first {
		t.Fatalf("repeat: %v", out)
	}

	call(t, "POST", pairingURL(base, tn.TenantID, "pairings", uuid.NewString(), "cancel"), provToken, nil, 404, nil)
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings", "nope", "cancel"), provToken, nil, 404, nil)

	// A code that was used is not cancellable.
	var used pairingBody
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), provToken, nil, 201, &used)
	if _, _, err := srv.Store.ExchangePairing(t.Context(), used.Code, store.DeviceInfo{Label: "x"}, *clock); err != nil {
		t.Fatal(err)
	}
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings", used.PairingID, "cancel"), provToken, nil, 404, nil)
}

func TestDevicesListAndRevoke(t *testing.T) {
	srv, base, clock := adminSetup(t, "")
	tn := newAdminTenant(t, base)
	list := pairingURL(base, tn.TenantID, "devices")

	var empty struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", list, provToken, nil, 200, &empty)
	if empty.Devices == nil || len(empty.Devices) != 0 {
		t.Fatalf("empty list must be [], got %#v", empty.Devices)
	}
	call(t, "GET", list, "", nil, 401, nil)

	a := pairDevice(t, srv, tn.TenantID, *clock)
	*clock = clock.Add(time.Minute)
	b := pairDevice(t, srv, tn.TenantID, *clock)

	var got struct {
		Devices []deviceBody `json:"devices"`
	}
	var raw map[string]any
	call(t, "GET", list, provToken, nil, 200, &got)
	call(t, "GET", list, provToken, nil, 200, &raw)
	if len(got.Devices) != 2 || got.Devices[0].DeviceID != b.ID.String() || got.Devices[1].DeviceID != a.ID.String() {
		t.Fatalf("want newest first [b a], got %+v", got.Devices)
	}
	d := got.Devices[1]
	if d.Label != "Samsung A15" || d.Operator != "Aidar" || d.RevokedAt != nil || d.LastSeenAt != nil {
		t.Fatalf("device %+v", d)
	}
	for _, dev := range raw["devices"].([]any) {
		for k := range dev.(map[string]any) {
			if strings.Contains(k, "key") || strings.Contains(k, "hash") {
				t.Fatalf("list must not show %q", k)
			}
		}
	}

	revoke := pairingURL(base, tn.TenantID, "devices", a.ID.String(), "revoke")
	call(t, "POST", revoke, "", nil, 401, nil)
	var r1, r2 deviceBody
	call(t, "POST", revoke, provToken, nil, 200, &r1)
	if r1.DeviceID != a.ID.String() || r1.RevokedAt == nil || *r1.RevokedAt != clock.UTC().Format(time.RFC3339) {
		t.Fatalf("revoke: %+v", r1)
	}
	*clock = clock.Add(time.Hour)
	call(t, "POST", revoke, provToken, nil, 200, &r2)
	if r2.RevokedAt == nil || *r2.RevokedAt != *r1.RevokedAt {
		t.Fatalf("repeat must keep the first time: %+v vs %+v", r2, r1)
	}
	// Revoked devices stay in the list.
	call(t, "GET", list, provToken, nil, 200, &got)
	if len(got.Devices) != 2 || got.Devices[1].RevokedAt == nil || got.Devices[0].RevokedAt != nil {
		t.Fatalf("after revoke: %+v", got.Devices)
	}

	call(t, "POST", pairingURL(base, tn.TenantID, "devices", uuid.NewString(), "revoke"), provToken, nil, 404, nil)
	call(t, "POST", pairingURL(base, tn.TenantID, "devices", "nope", "revoke"), provToken, nil, 404, nil)
	call(t, "GET", pairingURL(base, uuid.NewString(), "devices"), provToken, nil, 404, nil)
}

func TestPairingAdminCrossTenantIs404(t *testing.T) {
	srv, base, clock := adminSetup(t, "")
	a := newAdminTenant(t, base)
	b := newAdminTenant(t, base)

	var p pairingBody
	call(t, "POST", pairingURL(base, a.TenantID, "pairings"), provToken, nil, 201, &p)
	dev := pairDevice(t, srv, a.TenantID, *clock)

	// Tenant B names tenant A's pairing and device: 404, and nothing changes.
	call(t, "POST", pairingURL(base, b.TenantID, "pairings", p.PairingID, "cancel"), provToken, nil, 404, nil)
	call(t, "POST", pairingURL(base, b.TenantID, "devices", dev.ID.String(), "revoke"), provToken, nil, 404, nil)
	var got struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", pairingURL(base, b.TenantID, "devices"), provToken, nil, 200, &got)
	if len(got.Devices) != 0 {
		t.Fatalf("tenant B sees %+v", got.Devices)
	}
	call(t, "GET", pairingURL(base, a.TenantID, "devices"), provToken, nil, 200, &got)
	if len(got.Devices) != 1 || got.Devices[0].RevokedAt != nil {
		t.Fatalf("tenant A's device was touched: %+v", got.Devices)
	}
	// The code of A is still pending: B's cancel did nothing.
	if _, _, err := srv.Store.ExchangePairing(t.Context(), p.Code, store.DeviceInfo{Label: "x"}, *clock); err != nil {
		t.Fatalf("A's code was cancelled by B: %v", err)
	}
}

func TestPairingAdminNeedsProvisioningToken(t *testing.T) {
	srv, base, _ := adminSetup(t, "owner-token")
	tn := newAdminTenant(t, base)
	// The shared ACCESS_TOKEN and the tenant's own code do not open the admin API.
	for _, tok := range []string{"owner-token", tn.AccessCode} {
		call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), tok, nil, 401, nil)
		call(t, "GET", pairingURL(base, tn.TenantID, "devices"), tok, nil, 401, nil)
	}
	// Without a provisioning token the routes do not exist.
	off := &Server{Store: srv.Store}
	h := off.Handler()
	for _, path := range []string{"/pairings", "/devices"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/admin/v1/tenants/"+tn.TenantID+path, nil)
		req.Header.Set("Authorization", "Bearer "+provToken)
		h.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("%s without provisioning token: %d", path, rec.Code)
		}
	}
}
