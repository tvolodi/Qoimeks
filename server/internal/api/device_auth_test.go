package api

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// pairViaExchange runs the exchange for the tenant and returns the device id and key.
func pairViaExchange(t *testing.T, base string, tn tenantBody) (id, key string) {
	t.Helper()
	pb := newPairingCode(t, base, tn.TenantID)
	res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "phone"})
	if res.status != 200 {
		t.Fatalf("exchange: %d %s", res.status, res.raw)
	}
	return res.body["device_id"].(string), res.body["device_key"].(string)
}

func deviceByID(t *testing.T, base string, tn tenantBody, id string) deviceBody {
	t.Helper()
	var list struct {
		Devices []deviceBody `json:"devices"`
	}
	call(t, "GET", pairingURL(base, tn.TenantID, "devices"), provToken, nil, 200, &list)
	for _, d := range list.Devices {
		if d.DeviceID == id {
			return d
		}
	}
	t.Fatalf("device %s not listed", id)
	return deviceBody{}
}

func TestDeviceKeyServesItsTenantSchema(t *testing.T) {
	srv, base, _, tn := pairSetup(t)
	seedCatalog(t, srv, tn.AccessCode, "ONLY-T")
	_, key := pairViaExchange(t, base, tn)
	if got := stockCodes(t, base, key); got != "ONLY-T" {
		t.Fatalf("device sees %q", got)
	}
}

func TestDeviceKeyWithSharedTokenSet(t *testing.T) {
	srv, base, _ := adminSetup(t, "shared-token")
	tn := newAdminTenant(t, base)
	seedCatalog(t, srv, tn.AccessCode, "ONLY-T")
	_, key := pairViaExchange(t, base, tn)
	if got := stockCodes(t, base, key); got != "ONLY-T" {
		t.Fatalf("device sees %q", got)
	}
	call(t, "GET", base+"/api/catalog", "", nil, 401, nil)
	call(t, "GET", base+"/api/catalog", "shared-token", nil, 200, nil)
	call(t, "GET", base+"/api/catalog", tn.AccessCode, nil, 200, nil)
}

func TestUnknownDeviceKeyIsUnauthorized(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	_, key := pairViaExchange(t, base, tn)
	var out map[string]string
	call(t, "GET", base+"/api/catalog", key+"x", nil, 401, &out)
	if out["error"] != "access code is missing or wrong" || out["reason"] != "" {
		t.Fatalf("body = %v", out)
	}
	call(t, "GET", base+"/api/catalog", "qdk_", nil, 401, nil)
}

func TestRevokedDeviceKeyIsRefusedWithReason(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	id, key := pairViaExchange(t, base, tn)
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
	call(t, "POST", pairingURL(base, tn.TenantID, "devices/"+id+"/revoke"), provToken, nil, 200, nil)
	var out map[string]string
	call(t, "GET", base+"/api/catalog", key, nil, 401, &out)
	if out["reason"] != "device_revoked" || out["error"] != "device revoked" {
		t.Fatalf("body = %v", out)
	}
	// the row is kept and the revoked key never touches it
	if d := deviceByID(t, base, tn, id); d.RevokedAt == nil {
		t.Fatal("device row lost its revoked mark")
	}
	// another device of the tenant is unaffected
	_, other := pairViaExchange(t, base, tn)
	call(t, "GET", base+"/api/catalog", other, nil, 200, nil)
}

func TestDeviceOfSuspendedTenantIsForbidden(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	_, key := pairViaExchange(t, base, tn)
	call(t, "POST", pairingURL(base, tn.TenantID, "suspend"), provToken, nil, 200, nil)
	call(t, "GET", base+"/api/catalog", key, nil, 403, nil)
	call(t, "POST", pairingURL(base, tn.TenantID, "resume"), provToken, nil, 200, nil)
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
}

func TestDeviceKeyIsNotAcceptedByAdminAPI(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	_, key := pairViaExchange(t, base, tn)
	call(t, "GET", pairingURL(base, tn.TenantID, "devices"), key, nil, 401, nil)
	call(t, "POST", pairingURL(base, tn.TenantID, "pairings"), key, nil, 401, nil)
}

func TestDeviceLastSeenIsThrottled(t *testing.T) {
	_, base, clock, tn := pairSetup(t)
	id, key := pairViaExchange(t, base, tn)
	if d := deviceByID(t, base, tn, id); d.LastSeenAt != nil {
		t.Fatalf("last_seen_at before any call: %v", *d.LastSeenAt)
	}
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
	first := *deviceByID(t, base, tn, id).LastSeenAt
	*clock = clock.Add(deviceTouchEvery - time.Second)
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
	if got := *deviceByID(t, base, tn, id).LastSeenAt; got != first {
		t.Fatalf("written again within the window: %s -> %s", first, got)
	}
	*clock = clock.Add(2 * time.Second)
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
	if got := *deviceByID(t, base, tn, id).LastSeenAt; got == first {
		t.Fatal("not written after the window")
	}
}

// A shared token that happens to start with qdk_ stays the shared token.
func TestSharedTokenWithDevicePrefixIsUnchanged(t *testing.T) {
	_, base, _ := adminSetup(t, "qdk_shared")
	call(t, "GET", base+"/api/catalog", "qdk_shared", nil, 200, nil)
	call(t, "GET", base+"/api/catalog", "qdk_other", nil, 401, nil)
}

func TestMappedSharedTokenUnchangedByDeviceBranch(t *testing.T) {
	srv, base, _ := adminSetup(t, "shared")
	body := newBody()
	var tn tenantBody
	call(t, "POST", base+"/admin/v1/tenants", provToken, body, 201, &tn)
	srv.AccessTokenTenant = uuid.MustParse(body["tenant_id"])
	seedCatalog(t, srv, tn.AccessCode, "ONLY-T")
	if got := stockCodes(t, base, "shared"); got != "ONLY-T" {
		t.Fatalf("shared token sees %q", got)
	}
	if got := stockCodes(t, base, tn.AccessCode); !strings.Contains(got, "ONLY-T") {
		t.Fatalf("tenant code sees %q", got)
	}
}

// The app percent-encodes X-App-Version; the device list shows it decoded, trimmed and
// cut to the exchange's limit, and a call without the header keeps the stored version.
func TestDeviceTouchStoresDecodedAppVersion(t *testing.T) {
	cases := []struct{ name, header, want string }{
		{"encoded", "0.3.1%2B7", "0.3.1+7"},
		{"spaces", "  0.3.2  ", "0.3.2"},
		{"too long", strings.Repeat("9", 300), strings.Repeat("9", pairingMaxVersion)},
		{"missing", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, base, _, tn := pairSetup(t)
			id, key := pairViaExchange(t, base, tn)
			var header map[string]string
			if c.header != "" {
				header = map[string]string{"X-App-Version": c.header}
			}
			send(t, "GET", base+"/api/catalog", key, header, "", nil, 200)
			if got := deviceByID(t, base, tn, id).AppVersion; got != c.want {
				t.Fatalf("app_version %q, want %q", got, c.want)
			}
		})
	}
}

func TestDeviceTouchWithoutHeaderKeepsPairedVersion(t *testing.T) {
	_, base, _, tn := pairSetup(t)
	pb := newPairingCode(t, base, tn.TenantID)
	res := exchange(t, base, "", map[string]string{"code": pb.Code, "device_label": "phone", "app_version": "0.3.1+7"})
	if res.status != 200 {
		t.Fatalf("exchange: %d %s", res.status, res.raw)
	}
	id, key := res.body["device_id"].(string), res.body["device_key"].(string)
	call(t, "GET", base+"/api/catalog", key, nil, 200, nil)
	if got := deviceByID(t, base, tn, id).AppVersion; got != "0.3.1+7" {
		t.Fatalf("app_version %q, want the paired 0.3.1+7", got)
	}
}
