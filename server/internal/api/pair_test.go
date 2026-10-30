package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store/storetest"
)

type exchangeResult struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

// exchange posts to the public exchange; realIP, when set, is sent as X-Real-IP.
func exchange(t *testing.T, base, realIP string, body any) exchangeResult {
	t.Helper()
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest("POST", base+"/pair/v1/exchange", &buf)
	if realIP != "" {
		req.Header.Set("X-Real-IP", realIP)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
		return exchangeResult{}
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	raw.ReadFrom(resp.Body)
	res := exchangeResult{status: resp.StatusCode, header: resp.Header, raw: raw.String()}
	json.Unmarshal(raw.Bytes(), &res.body)
	return res
}

func pairSetup(t *testing.T) (*Server, string, *time.Time, tenantBody) {
	t.Helper()
	srv, base, clock := adminSetup(t, "")
	srv.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	return srv, base, clock, newAdminTenant(t, base)
}

func newPairingCode(t *testing.T, base, tenant string) pairingBody {
	t.Helper()
	var pb pairingBody
	call(t, "POST", pairingURL(base, tenant, "pairings"), provToken, nil, 201, &pb)
	return pb
}

func TestExchangeIssuesDeviceKeyOnce(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	pb := newPairingCode(t, base, tn.TenantID)
	res := exchange(t, base, "", map[string]string{"code": strings.ToLower(pb.Code), "device_label": "Samsung A15", "operator": "Aidar", "app_version": "0.2.0"})
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.raw)
	}
	if res.header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", res.header.Get("Cache-Control"))
	}
	key, _ := res.body["device_key"].(string)
	if !strings.HasPrefix(key, "qdk_") {
		t.Fatalf("device_key = %q", key)
	}
	if res.body["tenant"].(map[string]any)["name"] != "T" {
		t.Errorf("tenant = %v", res.body["tenant"])
	}
	// the device is listed, without any part of the key
	var list struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", pairingURL(base, tn.TenantID, "devices"), provToken, nil, 200, &list)
	if len(list.Devices) != 1 || list.Devices[0].DeviceID != res.body["device_id"] || list.Devices[0].Label != "Samsung A15" || list.Devices[0].Operator != "Aidar" {
		t.Fatalf("devices = %+v", list.Devices)
	}
	// the code is spent
	again := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "x"})
	if again.status != 400 || again.body["error"] != "invalid_pairing_code" {
		t.Fatalf("second use: %d %s", again.status, again.raw)
	}
}

func TestExchangeUniformErrorForWrongUsedExpiredCancelled(t *testing.T) {
	_, base, clock, tn := pairSetup(t)
	used := newPairingCode(t, base, tn.TenantID)
	exchange(t, base, "", map[string]string{"code": used.Code, "device_label": "a"})
	expired := newPairingCode(t, base, tn.TenantID)
	cancelled := newPairingCode(t, base, tn.TenantID)
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings", cancelled.PairingID, "cancel"), provToken, nil, 200, nil)
	*clock = clock.Add(11 * time.Minute)
	fresh := newPairingCode(t, base, tn.TenantID)
	_ = fresh

	var first string
	for name, code := range map[string]string{
		"wrong":     "ZZZZ-ZZZZ",
		"lookalike": "0O1I-LUUU",
		"short":     "AB",
		"used":      used.Code,
		"expired":   expired.Code,
		"cancelled": cancelled.Code,
	} {
		res := exchange(t, base, "10.0.0.1", map[string]string{"code": code, "device_label": "a"})
		if res.status != 400 || res.body["error"] != "invalid_pairing_code" {
			t.Errorf("%s: %d %s", name, res.status, res.raw)
		}
		if first == "" {
			first = res.raw
		} else if res.raw != first {
			t.Errorf("%s: body differs from the others: %s vs %s", name, res.raw, first)
		}
	}
}

func TestExchangeBadRequestsDoNotSpendTheCode(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	pb := newPairingCode(t, base, tn.TenantID)
	long := strings.Repeat("x", 81)
	for name, body := range map[string]any{
		"bad json":      "{",
		"no code":       map[string]string{"device_label": "a"},
		"no label":      map[string]string{"code": pb.Code},
		"long label":    map[string]string{"code": pb.Code, "device_label": long},
		"long operator": map[string]string{"code": pb.Code, "device_label": "a", "operator": long},
	} {
		res := exchange(t, base, "", body)
		if res.status != 400 || res.body["error"] != "invalid_request" {
			t.Errorf("%s: %d %s", name, res.status, res.raw)
		}
	}
	if res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "a"}); res.status != 200 {
		t.Fatalf("code was consumed by a bad request: %d %s", res.status, res.raw)
	}
}

func TestExchangeSingleUseUnderRace(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	pb := newPairingCode(t, base, tn.TenantID)
	const n = 10
	results := make([]exchangeResult, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = exchange(t, base, "10.0.1."+strconv.Itoa(i), map[string]string{"code": pb.Code, "device_label": "phone" + strconv.Itoa(i)})
		}()
	}
	wg.Wait()
	won := 0
	for _, r := range results {
		switch r.status {
		case 200:
			won++
		case 400:
			if r.body["error"] != "invalid_pairing_code" {
				t.Errorf("loser: %s", r.raw)
			}
		default:
			t.Errorf("unexpected %d: %s", r.status, r.raw)
		}
	}
	if won != 1 {
		t.Fatalf("%d winners, want 1", won)
	}
	var list struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", pairingURL(base, tn.TenantID, "devices"), provToken, nil, 200, &list)
	if len(list.Devices) != 1 {
		t.Fatalf("%d devices, want 1", len(list.Devices))
	}
}

func TestExchangeExpiryByClock(t *testing.T) {
	_, base, clock, tn := pairSetup(t)
	pb := newPairingCode(t, base, tn.TenantID)
	*clock = clock.Add(10*time.Minute + time.Second)
	if res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "a"}); res.status != 400 || res.body["error"] != "invalid_pairing_code" {
		t.Fatalf("%d %s", res.status, res.raw)
	}
}

func TestExchangeSuspendedAndExpiredTenantRefused(t *testing.T) {
	for _, verb := range []string{"suspend", "expire"} {
		t.Run(verb, func(t *testing.T) {
			_, base, _, tn := pairSetup(t)
			pb := newPairingCode(t, base, tn.TenantID)
			call(t, "POST", pairingURL(base, tn.TenantID, verb), provToken, nil, 200, nil)
			res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "a"})
			if res.status != 403 || res.body["error"] != "tenant_unavailable" {
				t.Fatalf("%d %s", res.status, res.raw)
			}
			if strings.Contains(res.raw, "qdk_") {
				t.Fatal("a key was issued")
			}
			var list struct {
				Devices []deviceBody `json:"devices"`
			}
			call(t, "GET", pairingURL(base, tn.TenantID, "devices"), provToken, nil, 200, &list)
			if len(list.Devices) != 0 {
				t.Fatalf("devices = %+v", list.Devices)
			}
			// the code is spent, even after the tenant is back
			if verb == "suspend" {
				call(t, "POST", pairingURL(base, tn.TenantID, "resume"), provToken, nil, 200, nil)
				if res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "a"}); res.status != 400 {
					t.Fatalf("spent code worked again: %d %s", res.status, res.raw)
				}
			}
		})
	}
}

func TestExchangeRateLimitedPerIPWithRetryAfter(t *testing.T) {
	_, base, clock, tn := pairSetup(t)
	bad := map[string]string{"code": "ZZZZ-ZZZZ", "device_label": "a"}
	for i := 0; i < 10; i++ {
		if res := exchange(t, base, "198.51.100.7", bad); res.status != 400 {
			t.Fatalf("attempt %d: %d %s", i, res.status, res.raw)
		}
		*clock = clock.Add(time.Second)
	}
	res := exchange(t, base, "198.51.100.7", bad)
	if res.status != 429 || res.body["error"] != "rate_limited" {
		t.Fatalf("%d %s", res.status, res.raw)
	}
	secs, err := strconv.Atoi(res.header.Get("Retry-After"))
	if err != nil || secs <= 0 || secs > 600 {
		t.Fatalf("Retry-After = %q", res.header.Get("Retry-After"))
	}
	// even a good code from that address waits; another address is not affected
	pb := newPairingCode(t, base, tn.TenantID)
	good := map[string]string{"code": pb.Code, "device_label": "a"}
	if res := exchange(t, base, "198.51.100.7", good); res.status != 429 {
		t.Fatalf("blocked IP: %d", res.status)
	}
	if res := exchange(t, base, "198.51.100.8", good); res.status != 200 {
		t.Fatalf("other IP: %d %s", res.status, res.raw)
	}
	// after the wait the address may try again
	*clock = clock.Add(time.Duration(secs) * time.Second)
	if res := exchange(t, base, "198.51.100.7", bad); res.status != 400 {
		t.Fatalf("after Retry-After: %d %s", res.status, res.raw)
	}
}

func TestExchangeIgnoresSpoofedRealIPFromUntrustedPeer(t *testing.T) {
	srv, base, _, _ := pairSetup(t)
	srv.TrustedProxies = nil // the peer 127.0.0.1 is no longer trusted: X-Real-IP is ignored
	bad := map[string]string{"code": "ZZZZ-ZZZZ", "device_label": "a"}
	for i := 0; i < 10; i++ {
		exchange(t, base, "203.0.113."+strconv.Itoa(i), bad)
	}
	if res := exchange(t, base, "203.0.113.200", bad); res.status != 429 {
		t.Fatalf("rotating X-Real-IP evaded the limit: %d", res.status)
	}
}

func TestExchangeGlobalFailureCap(t *testing.T) {
	_, base, _, _ := pairSetup(t)
	bad := map[string]string{"code": "ZZZZ-ZZZZ", "device_label": "a"}
	// 30 addresses x 10 failures = 300 failures, each address within its own limit
	for i := 0; i < 30; i++ {
		for j := 0; j < 10; j++ {
			if res := exchange(t, base, "10.9."+strconv.Itoa(i)+".1", bad); res.status != 400 {
				t.Fatalf("addr %d try %d: %d", i, j, res.status)
			}
		}
	}
	if res := exchange(t, base, "10.99.0.1", bad); res.status != 429 {
		t.Fatalf("global cap: %d", res.status)
	}
}

func TestExchangeDeviceLimitKeepsCodeUsable(t *testing.T) {
	srv, base, clock, tn := pairSetup(t)
	for i := 0; i < 50; i++ {
		pairDevice(t, srv, tn.TenantID, *clock)
	}
	id := uuid.MustParse(tn.TenantID)
	_, code, err := srv.Store.CreatePairing(t.Context(), id, "", 0, *clock)
	if err != nil {
		t.Fatal(err)
	}
	res := exchange(t, base, "", map[string]string{"code": code, "device_label": "a"})
	if res.status != 409 || res.body["error"] != "too_many_devices" {
		t.Fatalf("%d %s", res.status, res.raw)
	}
}

func TestExchangeOffWithoutProvisioningToken(t *testing.T) {
	srv := &Server{Store: storetest.New(t)}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	if res := exchange(t, ts.URL, "", map[string]string{"code": "AAAA-BBBB", "device_label": "a"}); res.status != 404 {
		t.Fatalf("%d", res.status)
	}
}

func TestExchangeCORS(t *testing.T) {
	_, base, _, _ := pairSetup(t)
	req, _ := http.NewRequest("OPTIONS", base+"/pair/v1/exchange", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Content-Type") ||
		!strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
	// the real response carries the origin header too
	res := exchange(t, base, "", map[string]string{"code": "ZZZZ-ZZZZ", "device_label": "a"})
	if res.header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("POST response lacks Access-Control-Allow-Origin")
	}
}

func TestExchangeDoesNotChangeOldAuth(t *testing.T) {
	_, base, _ := adminSetup(t, "shared-token")
	tn := newAdminTenant(t, base)
	// the exchange needs no bearer although ACCESS_TOKEN is set
	pb := newPairingCode(t, base, tn.TenantID)
	if res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "a"}); res.status != 200 {
		t.Fatalf("%d", res.status)
	}
	// /api/ still asks for the old credentials and accepts them
	call(t, "GET", base+"/api/catalog", "", nil, 401, nil)
	call(t, "GET", base+"/api/catalog", "shared-token", nil, 200, nil)
	call(t, "GET", base+"/api/catalog", tn.AccessCode, nil, 200, nil)
}

func TestExchangeKeepsPairingLabel(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	var pb pairingBody
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), provToken, map[string]any{"label": "uat web 1"}, 201, &pb)
	res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "web"})
	if res.status != 200 || res.body["label"] != "uat web 1" {
		t.Fatalf("exchange: %d %s", res.status, res.raw)
	}
	var list struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", pairingURL(base, tn.TenantID, "devices"), provToken, nil, 200, &list)
	if len(list.Devices) != 1 || list.Devices[0].Label != "uat web 1" {
		t.Fatalf("devices = %+v", list.Devices)
	}
}
