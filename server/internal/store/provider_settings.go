package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrTenantSchema: provider settings are server-wide and live in the default schema only.
var ErrTenantSchema = errors.New("provider settings exist only in the default schema")

// ProviderSettingRow is one stored row. KeyEnc is the encrypted API key (nil: none stored).
type ProviderSettingRow struct {
	Kind      string // "llm" or "stt"
	Provider  string
	Model     string
	KeyEnc    []byte
	KeyHint   string
	KeepKey   bool // upsert only: leave the stored key and hint as they are
	UpdatedAt time.Time
}

// UpsertProviderSetting writes the row of r.Kind. It refuses a tenant store before any SQL.
func (s *Store) UpsertProviderSetting(ctx context.Context, r ProviderSettingRow) error {
	if s.schema != "" {
		return ErrTenantSchema
	}
	if r.KeepKey {
		_, err := s.pool.Exec(ctx, `
			insert into provider_settings (kind, provider, model) values ($1, $2, $3)
			on conflict (kind) do update set provider = excluded.provider, model = excluded.model, updated_at = now()`,
			r.Kind, r.Provider, r.Model)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		insert into provider_settings (kind, provider, model, api_key_enc, key_hint) values ($1, $2, $3, $4, $5)
		on conflict (kind) do update set provider = excluded.provider, model = excluded.model,
			api_key_enc = excluded.api_key_enc, key_hint = excluded.key_hint, updated_at = now()`,
		r.Kind, r.Provider, r.Model, r.KeyEnc, r.KeyHint)
	return err
}

// GetProviderSetting returns the row of kind, or ErrNotFound.
func (s *Store) GetProviderSetting(ctx context.Context, kind string) (ProviderSettingRow, error) {
	if s.schema != "" {
		return ProviderSettingRow{}, ErrTenantSchema
	}
	var r ProviderSettingRow
	err := s.pool.QueryRow(ctx,
		`select kind, provider, model, api_key_enc, key_hint, updated_at from provider_settings where kind = $1`, kind,
	).Scan(&r.Kind, &r.Provider, &r.Model, &r.KeyEnc, &r.KeyHint, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// ListProviderSettings returns all rows ordered by kind.
func (s *Store) ListProviderSettings(ctx context.Context) ([]ProviderSettingRow, error) {
	if s.schema != "" {
		return nil, ErrTenantSchema
	}
	rows, err := s.pool.Query(ctx,
		`select kind, provider, model, api_key_enc, key_hint, updated_at from provider_settings order by kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderSettingRow
	for rows.Next() {
		var r ProviderSettingRow
		if err := rows.Scan(&r.Kind, &r.Provider, &r.Model, &r.KeyEnc, &r.KeyHint, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
