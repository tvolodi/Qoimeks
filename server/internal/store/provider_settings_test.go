package store_test

import (
	"context"
	"errors"
	"testing"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func TestProviderSettingsOnlyInDefaultSchema(t *testing.T) {
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	ts := tenantStore(t, st, tn)
	var def, ten *string
	ctx := context.Background()
	if err := st.Pool().QueryRow(ctx, `select to_regclass('public.provider_settings')::text`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool().QueryRow(ctx, `select to_regclass($1 || '.provider_settings')::text`, tn.SchemaName).Scan(&ten); err != nil {
		t.Fatal(err)
	}
	if def == nil {
		t.Error("provider_settings is missing in the default schema")
	}
	if ten != nil {
		t.Errorf("provider_settings exists in the tenant schema: %s", *ten)
	}
	if n := count(t, ts, `select count(*) from schema_migrations where name = '009_provider_settings.sql'`); n != 0 {
		t.Errorf("tenant schema recorded migration 009: %d", n)
	}
}

func TestProviderSettingsWriterRefusesTenantStore(t *testing.T) {
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	ts := tenantStore(t, st, tn)
	ctx := context.Background()
	row := store.ProviderSettingRow{Kind: "llm", Provider: "openai"}
	if err := ts.UpsertProviderSetting(ctx, row); !errors.Is(err, store.ErrTenantSchema) {
		t.Errorf("upsert err = %v", err)
	}
	if _, err := ts.GetProviderSetting(ctx, "llm"); !errors.Is(err, store.ErrTenantSchema) {
		t.Errorf("get err = %v", err)
	}
	rows, err := st.ListProviderSettings(ctx)
	if err != nil || len(rows) != 0 {
		t.Errorf("default rows = %v, %v", rows, err)
	}
}

func TestProviderSettingsUpsertKeepKey(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if _, err := st.GetProviderSetting(ctx, "llm"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get err = %v", err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.UpsertProviderSetting(ctx, store.ProviderSettingRow{Kind: "llm", Provider: "openai", Model: "a", KeyEnc: []byte{1, 2}, KeyHint: "wxyz"}))
	must(st.UpsertProviderSetting(ctx, store.ProviderSettingRow{Kind: "llm", Provider: "xai", Model: "b", KeepKey: true}))
	r, err := st.GetProviderSetting(ctx, "llm")
	must(err)
	if r.Provider != "xai" || r.Model != "b" || string(r.KeyEnc) != "\x01\x02" || r.KeyHint != "wxyz" {
		t.Errorf("row = %+v", r)
	}
	must(st.UpsertProviderSetting(ctx, store.ProviderSettingRow{Kind: "llm", Provider: "xai", Model: "b"}))
	r, _ = st.GetProviderSetting(ctx, "llm")
	if r.KeyEnc != nil || r.KeyHint != "" {
		t.Errorf("key not cleared: %+v", r)
	}
}
