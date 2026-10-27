package store_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func TestExpiryMigrationKeepsExistingRows(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	schema := "t_expiry_migration"
	if _, err := st.Pool().Exec(ctx, `create schema `+schema); err != nil {
		t.Fatal(err)
	}
	ts, err := st.ForSchema(ctx, schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)

	before := fstest.MapFS{}
	after := fstest.MapFS{}
	for _, name := range []string{"005_tenants.sql", "006_tenants_drop_access_code.sql", "007_tenants_expiry.sql"} {
		data, err := os.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		after[name] = &fstest.MapFile{Data: data}
		if !strings.HasPrefix(name, "007") {
			before[name] = &fstest.MapFile{Data: data}
		}
	}
	if err := ts.MigrateFS(ctx, before, false); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := ts.Pool().Exec(ctx, `insert into tenants (tenant_id, name, language, tier, schema_name, access_hash)
		values ($1, 'old', 'ru', 'demo', 't_old', '\x01')`, id); err != nil {
		t.Fatal(err)
	}
	if err := ts.MigrateFS(ctx, after, false); err != nil {
		t.Fatal(err)
	}
	var status string
	var expires *time.Time
	if err := ts.Pool().QueryRow(ctx, `select status, expires_at from tenants where tenant_id = $1`, id).Scan(&status, &expires); err != nil {
		t.Fatal(err)
	}
	if status != "active" || expires != nil {
		t.Fatalf("old row: status %q expires %v", status, expires)
	}
	if _, err := ts.Pool().Exec(ctx, `update tenants set status = 'expired' where tenant_id = $1`, id); err != nil {
		t.Fatalf("expired status not allowed: %v", err)
	}
	if _, err := ts.Pool().Exec(ctx, `update tenants set status = 'bogus' where tenant_id = $1`, id); err == nil {
		t.Fatal("bogus status accepted")
	}
}

func TestCreateTenantExpiresAt(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-time.Hour)
	tests := map[string]struct {
		tier string
		at   *time.Time
		ok   bool
	}{
		"demo future":      {"demo", &future, true},
		"demo none":        {"demo", nil, true},
		"demo past":        {"demo", &past, false},
		"demo now":         {"demo", &now, false},
		"production":       {"production", &future, false},
		"legacy hosted":    {"hosted_own_keys", &future, false},
		"production, none": {"production", nil, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tn, _, _, err := st.CreateTenant(ctx, store.TenantInput{
				ID: uuid.NewString(), Name: "n", Language: "ru", Tier: tc.tier, ExpiresAt: tc.at, Now: now,
			})
			if !tc.ok {
				if !errors.Is(err, store.ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (tc.at == nil) != (tn.ExpiresAt == nil) || tc.at != nil && !tn.ExpiresAt.Equal(*tc.at) {
				t.Fatalf("expires_at = %v, want %v", tn.ExpiresAt, tc.at)
			}
		})
	}

	// A repeat ignores a changed expires_at.
	in := store.TenantInput{ID: uuid.NewString(), Name: "n", Language: "ru", Tier: "demo", ExpiresAt: &future, Now: now}
	if _, _, _, err := st.CreateTenant(ctx, in); err != nil {
		t.Fatal(err)
	}
	later := future.Add(48 * time.Hour)
	in.ExpiresAt = &later
	tn, _, created, err := st.CreateTenant(ctx, in)
	if err != nil || created || !tn.ExpiresAt.Equal(future) {
		t.Fatalf("repeat: created=%v expires=%v err=%v", created, tn.ExpiresAt, err)
	}
}

func TestExpiredTenantStatus(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	now := time.Now()
	tn, _ := newTenant(t, st)

	if err := st.SetTenantStatus(ctx, tn.ID, store.TenantExpired, now); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTenantStatus(ctx, tn.ID, store.TenantExpired, now); err != nil { // idempotent
		t.Fatal(err)
	}
	for _, status := range []string{store.TenantActive, store.TenantSuspended} {
		if err := st.SetTenantStatus(ctx, tn.ID, status, now); !errors.Is(err, store.ErrTenantExpired) {
			t.Fatalf("%s on expired: %v", status, err)
		}
	}
	if got, _ := st.Tenant(ctx, tn.ID); got.Status != store.TenantExpired {
		t.Fatalf("status %q", got.Status)
	}
	if err := st.SetTenantStatus(ctx, uuid.New(), store.TenantExpired, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	// A date that has passed is expired without the call.
	tn2, _ := newTenant(t, st)
	if _, err := st.Pool().Exec(ctx, `update tenants set expires_at = $2 where tenant_id = $1`, tn2.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Tenant(ctx, tn2.ID)
	if got.Status != store.TenantActive || got.EffectiveStatus(now) != store.TenantExpired {
		t.Fatalf("stored %q effective %q", got.Status, got.EffectiveStatus(now))
	}
	if err := st.SetTenantStatus(ctx, tn2.ID, store.TenantActive, now); !errors.Is(err, store.ErrTenantExpired) {
		t.Fatalf("resume past date: %v", err)
	}
}
