package store_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestNewPairingCodeFormatAndAlphabet(t *testing.T) {
	seen := map[byte]bool{}
	for i := 0; i < 500; i++ {
		code, err := store.NewPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("bad shape %q", code)
		}
		norm, ok := store.NormalizePairingCode(code)
		if !ok || norm != strings.ReplaceAll(code, "-", "") {
			t.Fatalf("generated code does not normalise: %q -> %q %v", code, norm, ok)
		}
		if strings.ContainsAny(norm, "01ILOU") {
			t.Fatalf("look-alike in %q", code)
		}
		for i := 0; i < len(norm); i++ {
			seen[norm[i]] = true
		}
	}
	if len(seen) != 30 {
		t.Fatalf("500 codes used %d of 30 characters", len(seen))
	}
}

func TestNormalizePairingCode(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"k7m2-9qxa", "K7M29QXA", true},
		{" K7M2 9QXA ", "K7M29QXA", true},
		{"K7M29QXA", "K7M29QXA", true},
		{"K7M2-9QX", "K7M29QX", false},     // short
		{"K7M2-9QXAB", "K7M29QXAB", false}, // long
		{"K7M2-9QX0", "K7M29QX0", false},   // 0 is not in the alphabet, not mapped to O
		{"K7M2-9QXO", "K7M29QXO", false},
		{"K7M2-9QX1", "K7M29QX1", false},
		{"K7M2-9QXI", "K7M29QXI", false},
		{"K7M2-9QXL", "K7M29QXL", false},
		{"K7M2-9QXU", "K7M29QXU", false},
		{"", "", false},
	} {
		got, ok := store.NormalizePairingCode(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: got %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNewDeviceKey(t *testing.T) {
	a, err := store.NewDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := store.NewDeviceKey()
	if !strings.HasPrefix(a, "qdk_") || len(a) != 4+52 || a == b {
		t.Fatalf("bad keys %q %q", a, b)
	}
}

func TestPairingTablesAreDefaultSchemaOnly(t *testing.T) {
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	if n := count(t, st, `select count(*) from information_schema.tables where table_schema = 'public' and table_name in ('device_pairings','devices')`); n != 2 {
		t.Fatalf("public tables: %d", n)
	}
	if n := count(t, st, `select count(*) from information_schema.tables where table_schema = $1 and table_name in ('device_pairings','devices')`, tn.SchemaName); n != 0 {
		t.Fatalf("tenant schema has %d pairing tables", n)
	}
}

func TestPairingExchangeCreatesDeviceAndStoresHashesOnly(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	p, code, err := st.CreatePairing(ctx, tn.ID, "Склад 1", 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !p.ExpiresAt.Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("default life: %v", p.ExpiresAt)
	}
	d, key, err := st.ExchangePairing(ctx, strings.ToLower(code), store.DeviceInfo{Operator: "Аскар", AppVersion: "0.2.0"}, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if d.TenantID != tn.ID || d.Label != "Склад 1" || d.Operator != "Аскар" || d.PairingID == nil || *d.PairingID != p.ID || d.RevokedAt != nil {
		t.Fatalf("device: %+v", d)
	}
	got, err := st.DeviceByKey(ctx, key)
	if err != nil || got.ID != d.ID {
		t.Fatalf("by key: %v %+v", err, got)
	}
	if _, err := st.DeviceByKey(ctx, key+"x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong key: %v", err)
	}
	// Nothing in the database holds the clear code or key.
	norm, _ := store.NormalizePairingCode(code)
	for _, q := range []string{
		`select count(*) from device_pairings where code_hash = convert_to($1, 'UTF8') or label like '%' || $1 || '%'`,
		`select count(*) from devices where key_hash = convert_to($1, 'UTF8') or label like '%' || $1 || '%'`,
	} {
		for _, secret := range []string{norm, key} {
			if n := count(t, st, q, secret); n != 0 {
				t.Fatalf("secret %q found in database", secret)
			}
		}
	}
	if n := count(t, st, `select count(*) from device_pairings where used_at is not null`); n != 1 {
		t.Fatalf("used rows: %d", n)
	}
}

func TestPairingCodeIsSingleUse(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	_, code, _ := st.CreatePairing(ctx, tn.ID, "", 0, t0)
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "a"}, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "b"}, t0); !errors.Is(err, store.ErrPairingCodeInvalid) {
		t.Fatalf("second use: %v", err)
	}
}

func TestPairingConcurrentDoubleExchangeHasOneWinner(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	_, code, _ := st.CreatePairing(ctx, tn.ID, "", 0, t0)
	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, errs[i] = st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "phone"}, t0)
		}()
	}
	close(start)
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, store.ErrPairingCodeInvalid):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners, want 1", wins)
	}
	if n := count(t, st, `select count(*) from devices`); n != 1 {
		t.Fatalf("devices: %d", n)
	}
}

func TestPairingExpiryUsesTheGivenClock(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	_, code, _ := st.CreatePairing(ctx, tn.ID, "", 0, t0)
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "late"}, t0.Add(10*time.Minute)); !errors.Is(err, store.ErrPairingCodeInvalid) {
		t.Fatalf("at expiry: %v", err)
	}
	_, code, _ = st.CreatePairing(ctx, tn.ID, "", 0, t0)
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "ok"}, t0.Add(10*time.Minute-time.Second)); err != nil {
		t.Fatalf("just before expiry: %v", err)
	}
}

func TestPairingCancelAndWrongInput(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	other, _ := newTenant(t, st)
	p, code, _ := st.CreatePairing(ctx, tn.ID, "", 0, t0)
	if err := st.CancelPairing(ctx, other.ID, p.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant cancel: %v", err)
	}
	if err := st.CancelPairing(ctx, tn.ID, p.ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := st.CancelPairing(ctx, tn.ID, p.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cancel twice: %v", err)
	}
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "x"}, t0); !errors.Is(err, store.ErrPairingCodeInvalid) {
		t.Fatalf("cancelled code: %v", err)
	}
	for _, bad := range []string{"", "ZZZZ-ZZZZ", "K7M2-9QX0"} {
		if _, _, err := st.ExchangePairing(ctx, bad, store.DeviceInfo{}, t0); !errors.Is(err, store.ErrPairingCodeInvalid) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	if _, _, err := st.CreatePairing(ctx, uuid.New(), "", 0, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown tenant: %v", err)
	}
}

func TestPairingPendingLimit(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	other, _ := newTenant(t, st)
	var first string
	var firstID uuid.UUID
	for i := 0; i < store.MaxPendingPairings; i++ {
		p, code, err := st.CreatePairing(ctx, tn.ID, "", 0, t0)
		if err != nil {
			t.Fatalf("code %d: %v", i, err)
		}
		if i == 0 {
			first, firstID = code, p.ID
		}
	}
	if _, _, err := st.CreatePairing(ctx, tn.ID, "", 0, t0); !errors.Is(err, store.ErrPendingLimit) {
		t.Fatalf("sixth: %v", err)
	}
	if _, _, err := st.CreatePairing(ctx, other.ID, "", 0, t0); err != nil {
		t.Fatalf("other tenant is not limited: %v", err)
	}
	// A cancelled, a used and an expired code stop counting.
	if err := st.CancelPairing(ctx, tn.ID, firstID, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreatePairing(ctx, tn.ID, "", 0, t0); err != nil {
		t.Fatalf("after cancel: %v", err)
	}
	_ = first
	if _, _, err := st.CreatePairing(ctx, tn.ID, "", 0, t0); !errors.Is(err, store.ErrPendingLimit) {
		t.Fatalf("full again: %v", err)
	}
	if _, _, err := st.CreatePairing(ctx, tn.ID, "", 0, t0.Add(11*time.Minute)); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestPairingConcurrentCreateKeepsPendingLimit(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	const racers = 12
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = st.CreatePairing(ctx, tn.ID, "", 0, t0)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, store.ErrPendingLimit) {
			t.Fatal(err)
		}
	}
	if ok != store.MaxPendingPairings {
		t.Fatalf("%d created, want %d", ok, store.MaxPendingPairings)
	}
}

func pairOne(t *testing.T, st *store.Store, tn store.Tenant, label string) (store.Device, string) {
	t.Helper()
	_, code, err := st.CreatePairing(t.Context(), tn.ID, "", 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	d, key, err := st.ExchangePairing(t.Context(), code, store.DeviceInfo{Label: label}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return d, key
}

func TestDeviceLimit(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	var first store.Device
	for i := 0; i < store.MaxDevices; i++ {
		d, _ := pairOne(t, st, tn, "d")
		if i == 0 {
			first = d
		}
	}
	_, code, err := st.CreatePairing(ctx, tn.ID, "", 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "51st"}, t0); !errors.Is(err, store.ErrDeviceLimit) {
		t.Fatalf("51st: %v", err)
	}
	// The failed exchange rolled back: the code is still good once a slot is free.
	if err := st.RevokeDevice(ctx, tn.ID, first.ID, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "51st"}, t0); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
	if devs, _ := st.ListDevices(ctx, tn.ID); len(devs) != store.MaxDevices+1 {
		t.Fatalf("revoked device must be kept, have %d", len(devs))
	}
}

func TestDeviceRevokeListTouch(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	other, _ := newTenant(t, st)
	a, keyA := pairOne(t, st, tn, "a")
	_, _ = pairOne(t, st, other, "o")
	devs, err := st.ListDevices(ctx, tn.ID)
	if err != nil || len(devs) != 1 || devs[0].ID != a.ID {
		t.Fatalf("list is per tenant: %v %+v", err, devs)
	}
	if err := st.RevokeDevice(ctx, other.ID, a.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant revoke: %v", err)
	}
	if err := st.TouchDevice(ctx, a.ID, "0.3.0", t0.Add(time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	// Within minAge nothing is written.
	if err := st.TouchDevice(ctx, a.ID, "0.4.0", t0.Add(time.Hour+30*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, _ := st.DeviceByKey(ctx, keyA)
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(t0.Add(time.Hour)) || got.AppVersion != "0.3.0" {
		t.Fatalf("touch: %+v", got)
	}
	if err := st.TouchDevice(ctx, a.ID, "", t0.Add(2*time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.DeviceByKey(ctx, keyA); got.AppVersion != "0.3.0" || !got.LastSeenAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("empty version keeps the old one: %+v", got)
	}
	if err := st.RevokeDevice(ctx, tn.ID, a.ID, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeDevice(ctx, tn.ID, a.ID, t0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchDevice(ctx, a.ID, "", t0.Add(5*time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err = st.DeviceByKey(ctx, keyA)
	if err != nil || got.RevokedAt == nil || !got.RevokedAt.Equal(t0.Add(3*time.Hour)) || !got.LastSeenAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("revoked device is kept, first time wins, not touched: %v %+v", err, got)
	}
	if err := st.RevokeDevice(ctx, tn.ID, uuid.New(), t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown device: %v", err)
	}
}

func TestPairingLabelWinsOverAppLabel(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	tn, _ := newTenant(t, st)
	_, code, err := st.CreatePairing(ctx, tn.ID, "uat web 1", 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := st.ExchangePairing(ctx, code, store.DeviceInfo{Label: "web"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Label != "uat web 1" {
		t.Fatalf("label = %q, want the pairing's", d.Label)
	}
	if d2, _ := pairOne(t, st, tn, "Samsung A15"); d2.Label != "Samsung A15" {
		t.Fatalf("unlabelled pairing: label = %q, want the app's", d2.Label)
	}
}
