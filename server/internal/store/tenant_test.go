package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func newTenant(t *testing.T, st *store.Store) (store.Tenant, string) {
	t.Helper()
	tn, code, created, err := st.CreateTenant(context.Background(), store.TenantInput{
		ID: uuid.NewString(), Name: "Склад", Language: "ru", Tier: "demo",
	})
	if err != nil || !created {
		t.Fatalf("create tenant: created=%v err=%v", created, err)
	}
	return tn, code
}

func tenantStore(t *testing.T, st *store.Store, tn store.Tenant) *store.Store {
	t.Helper()
	ts, err := st.TenantStore(context.Background(), tn.SchemaName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	return ts
}

func count(t *testing.T, st *store.Store, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoredTierMapping(t *testing.T) {
	for _, c := range []struct{ tier, plan, want string }{
		{"demo", "", "demo"},
		{"demo", "all_inclusive", "demo"},
		{"demo", "bogus", "demo"},
		{"production", "", "hosted_own_keys"},
		{"production", "own_keys", "hosted_own_keys"},
		{"production", "all_inclusive", "hosted_all_inclusive"},
		{"hosted_own_keys", "all_inclusive", "hosted_own_keys"},
		{"hosted_all_inclusive", "own_keys", "hosted_all_inclusive"},
	} {
		got, err := store.StoredTier(c.tier, c.plan)
		if err != nil || got != c.want {
			t.Errorf("StoredTier(%q,%q) = %q, %v; want %q", c.tier, c.plan, got, err, c.want)
		}
	}
	for _, c := range [][2]string{{"production", "gold"}, {"gold", ""}, {"", ""}} {
		if _, err := store.StoredTier(c[0], c[1]); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("StoredTier(%q,%q) err = %v; want ErrInvalid", c[0], c[1], err)
		}
	}
}

func TestCreateTenantStoresMappedTier(t *testing.T) {
	st := storetest.New(t)
	for _, c := range []struct{ tier, plan, want string }{
		{"production", "all_inclusive", "hosted_all_inclusive"},
		{"production", "", "hosted_own_keys"},
	} {
		tn, _, _, err := st.CreateTenant(context.Background(), store.TenantInput{
			ID: uuid.NewString(), Name: "X", Language: "ru", Tier: c.tier, Plan: c.plan,
		})
		if err != nil || tn.Tier != c.want {
			t.Errorf("%s/%s: tier=%q err=%v; want %q", c.tier, c.plan, tn.Tier, err, c.want)
		}
	}
}

func TestCreateTenantIsIdempotentAndBuildsLedger(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	in := store.TenantInput{ID: uuid.NewString(), Name: "Склад", Language: "kk", Tier: "hosted_own_keys", OwnerEmail: "a@b.kz"}
	first, code, created, err := st.CreateTenant(ctx, in)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	in.Name, in.Tier = "Другое", "demo"
	again, againCode, created, err := st.CreateTenant(ctx, in)
	if err != nil || created {
		t.Fatalf("repeat: created=%v err=%v", created, err)
	}
	if againCode != "" || again.Name != "Склад" || again.Tier != "hosted_own_keys" {
		t.Fatalf("repeat changed the tenant: %+v vs %+v", again, first)
	}
	if code == "" || first.Status != store.TenantActive {
		t.Fatalf("bad tenant: %+v", first)
	}
	if n := count(t, st, `select count(*) from information_schema.schemata where schema_name like 't\_%'`); n != 1 {
		t.Fatalf("schemas: %d", n)
	}

	// The ledger tables and their append-only triggers exist inside the tenant schema.
	ts := tenantStore(t, st, first)
	if _, err := ts.Pool().Exec(ctx, `truncate movements`); err == nil {
		t.Fatal("truncate of tenant movements was allowed")
	}
	if _, err := ts.Pool().Exec(ctx, `truncate imports`); err == nil {
		t.Fatal("truncate of tenant imports was allowed")
	}
	// The tenant cannot see the registry.
	if _, err := ts.Pool().Exec(ctx, `select 1 from tenants`); err == nil {
		t.Fatal("tenant schema sees the registry")
	}
}

func TestCreateTenantValidation(t *testing.T) {
	st := storetest.New(t)
	tests := map[string]func(*store.TenantInput){
		"bad uuid":     func(i *store.TenantInput) { i.ID = "nope" },
		"empty name":   func(i *store.TenantInput) { i.Name = "  " },
		"long name":    func(i *store.TenantInput) { i.Name = strings.Repeat("я", 201) },
		"bad language": func(i *store.TenantInput) { i.Language = "de" },
		"bad tier":     func(i *store.TenantInput) { i.Tier = "free" },
		"long email":   func(i *store.TenantInput) { i.OwnerEmail = strings.Repeat("a", 255) },
	}
	for name, mod := range tests {
		t.Run(name, func(t *testing.T) {
			in := store.TenantInput{ID: uuid.NewString(), Name: "n", Language: "ru", Tier: "demo"}
			mod(&in)
			if _, _, _, err := st.CreateTenant(context.Background(), in); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestTenantStatusAndLookup(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	tn, code := newTenant(t, st)

	got, err := st.TenantByCode(ctx, code)
	if err != nil || got.ID != tn.ID {
		t.Fatalf("by code: %+v %v", got, err)
	}
	if _, err := st.TenantByCode(ctx, "wrong"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("miss: %v", err)
	}
	for _, status := range []string{store.TenantSuspended, store.TenantSuspended, store.TenantActive} {
		if err := st.SetTenantStatus(ctx, tn.ID, status, time.Now()); err != nil {
			t.Fatal(err)
		}
		if got, _ := st.Tenant(ctx, tn.ID); got.Status != status {
			t.Fatalf("status %q, want %q", got.Status, status)
		}
	}
	if err := st.SetTenantStatus(ctx, uuid.New(), store.TenantActive, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := st.Tenant(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestTenantsDoNotShareStock(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	a, _ := newTenant(t, st)
	b, _ := newTenant(t, st)
	sa, sb := tenantStore(t, st, a), tenantStore(t, st, b)

	var skuID, locID int64
	if err := sa.Pool().QueryRow(ctx, `insert into skus (code, name) values ('ONLY-A', 'a') returning id`).Scan(&skuID); err != nil {
		t.Fatal(err)
	}
	if err := sa.Pool().QueryRow(ctx, `insert into locations (code) values ('A-1') returning id`).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sa.Post(ctx, store.NewMovement{
		ID: uuid.New(), Kind: store.KindReceipt, SKUID: skuID, LocationID: locID, Qty: 5, Operator: "x", Source: "keyboard",
	}); err != nil {
		t.Fatal(err)
	}
	for name, other := range map[string]*store.Store{"tenant B": sb, "default": st} {
		items, err := other.Stock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.Code == "ONLY-A" {
				t.Fatalf("%s sees tenant A's SKU", name)
			}
		}
	}
	items, err := sa.Stock(ctx)
	if err != nil || len(items) != 1 || items[0].Total != 5 {
		t.Fatalf("tenant A stock: %+v %v", items, err)
	}
}

func TestConcurrentCreateTenant(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	in := store.TenantInput{ID: uuid.NewString(), Name: "n", Language: "ru", Tier: "demo"}
	const n = 4
	var wg sync.WaitGroup
	created := make([]bool, n)
	codes := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, codes[i], created[i], errs[i] = st.CreateTenant(ctx, in)
		}()
	}
	wg.Wait()
	made := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		if created[i] {
			made++
		}
		if created[i] != (codes[i] != "") {
			t.Fatalf("call %d: created=%v but code=%q", i, created[i], codes[i])
		}
	}
	if made != 1 {
		t.Fatalf("%d calls created the tenant, want 1", made)
	}
	var tn store.Tenant
	for i := range n {
		if created[i] {
			tn, _ = st.TenantByCode(ctx, codes[i])
		}
	}
	if c := count(t, st, `select count(*) from information_schema.tables where table_schema = $1 and table_name in ('skus','locations','movements','balances','imports')`, tn.SchemaName); c != 5 {
		t.Fatalf("tenant schema has %d of 5 tables", c)
	}
}

func TestLaterMigrationReachesTenants(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	ts := tenantStore(t, st, tn)

	fsys := fstest.MapFS{
		"009_later.sql":    {Data: []byte(`create table later_table (id int)`)},
		"010_registry.sql": {Data: []byte(`create table only_default (id int)`)},
	}
	defer store.SetPublicOnly("010_registry.sql")()
	if err := ts.MigrateFS(ctx, fsys, true); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ts, `select count(*) from schema_migrations where name = '009_later.sql'`); n != 1 {
		t.Fatalf("009 recorded %d times", n)
	}
	if _, err := ts.Pool().Exec(ctx, `select 1 from later_table`); err != nil {
		t.Fatalf("tenant lacks later_table: %v", err)
	}
	if _, err := ts.Pool().Exec(ctx, `select 1 from only_default`); err == nil {
		t.Fatal("public-only migration ran in the tenant schema")
	}
}

func TestMigrateTenantsCoversSuspended(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	if err := st.SetTenantStatus(ctx, tn.ID, store.TenantSuspended, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Simulate a tenant created before migration 004 existed.
	ts := tenantStore(t, st, tn)
	if _, err := ts.Pool().Exec(ctx, `delete from schema_migrations where name = '004_import_movements.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Pool().Exec(ctx, `alter table movements drop column import_id`); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := st.MigrateTenants(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ts, `select count(*) from information_schema.columns where table_schema = $1 and table_name = 'movements' and column_name = 'import_id'`, tn.SchemaName); n != 1 {
		t.Fatal("suspended tenant was not migrated")
	}
}

func TestConcurrentMigrateOnFreshSchema(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if _, err := st.Pool().Exec(ctx, `create schema fresh`); err != nil {
		t.Fatal(err)
	}
	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts, err := st.TenantStore(ctx, "fresh")
			if err == nil {
				ts.Close()
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccessCodeColumnDroppedAndOldCodesStillWork(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	// Rewind to the state under migration 005: a clear-text column beside the hash.
	if _, err := st.Pool().Exec(ctx, `alter table tenants add column access_code text not null default ''`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx, `delete from schema_migrations where name = '006_tenants_drop_access_code.sql'`); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	const old = "legacy-code-1234"
	if _, err := st.Pool().Exec(ctx, `
		insert into tenants (tenant_id, name, language, tier, schema_name, access_hash, access_code)
		values ($1, 'Старый', 'ru', 'demo', $2, sha256($3::bytea), $3)`,
		id, "t_"+strings.ReplaceAll(id.String(), "-", ""), old); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `select count(*) from information_schema.columns where table_name = 'tenants' and column_name = 'access_code'`); n != 0 {
		t.Fatal("access_code column still exists")
	}
	got, err := st.TenantByCode(ctx, old)
	if err != nil || got.ID != id {
		t.Fatalf("old code after migration: %+v %v", got, err)
	}
}

func TestCreateTenantReturnsCodeOnlyOnce(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	in := store.TenantInput{ID: uuid.NewString(), Name: "n", Language: "ru", Tier: "demo"}
	_, code, created, err := st.CreateTenant(ctx, in)
	if err != nil || !created || code == "" {
		t.Fatalf("first: code=%q created=%v err=%v", code, created, err)
	}
	if n := count(t, st, `select count(*) from tenants where access_hash = sha256($1::bytea)`, code); n != 1 {
		t.Fatal("hash of the returned code is not stored")
	}
	_, code2, created, err := st.CreateTenant(ctx, in)
	if err != nil || created || code2 != "" {
		t.Fatalf("repeat: code=%q created=%v err=%v", code2, created, err)
	}
}

func TestRotateTenantCode(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	tn, old := newTenant(t, st)
	if err := st.SetTenantStatus(ctx, tn.ID, store.TenantSuspended, time.Now()); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.RotateTenantCode(ctx, tn.ID)
	if err != nil || fresh == "" || fresh == old {
		t.Fatalf("rotate: %q %v", fresh, err)
	}
	if _, err := st.TenantByCode(ctx, old); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old code: %v", err)
	}
	if got, err := st.TenantByCode(ctx, fresh); err != nil || got.ID != tn.ID || got.Status != store.TenantSuspended {
		t.Fatalf("new code: %+v %v", got, err)
	}
	again, err := st.RotateTenantCode(ctx, tn.ID)
	if err != nil || again == fresh {
		t.Fatalf("second rotate: %q %v", again, err)
	}
	if _, err := st.RotateTenantCode(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}
